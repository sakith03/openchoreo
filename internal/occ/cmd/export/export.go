// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package export

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/openchoreo/openchoreo/internal/occ/cmd/config"
	"github.com/openchoreo/openchoreo/internal/occ/resources/client"
)

// Run prepares the complete export before transactionally synchronizing managed
// files. Layouts determine ownership; no inventory is written into the output.
func Run(ctx context.Context, c client.Interface, params Params) ([]SerializedFile, error) {
	if err := params.validate(); err != nil {
		return nil, err
	}
	directory, server, err := exportSource(params.OutputDir)
	if err != nil {
		return nil, err
	}
	resources, err := Fetch(ctx, c)
	if err != nil {
		return nil, err
	}
	filtered, err := Filter(ctx, resources)
	if err != nil {
		return nil, err
	}
	canonical, err := Canonicalize(ctx, filtered)
	if err != nil {
		return nil, err
	}
	files, err := Serialize(ctx, canonical)
	if err != nil {
		return nil, err
	}
	options := writeOptions{
		confirmEmpty: params.ConfirmEmpty,
		commit:       func() error { return bindExportSource(directory, server) },
	}
	if err := writeFiles(ctx, directory, files, options); err != nil {
		return nil, err
	}
	return files, nil
}

func exportSource(outputDir string) (string, string, error) {
	directory, err := canonicalOutputDir(outputDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve output directory: %w", err)
	}
	cfg, err := config.LoadStoredConfig()
	if err != nil {
		return "", "", err
	}
	plane, err := cfg.ActiveControlPlane()
	if err != nil {
		return "", "", err
	}
	server, err := url.Parse(plane.URL)
	if err != nil || server.Host == "" || (server.Scheme != "http" && server.Scheme != "https") || server.User != nil {
		return "", "", fmt.Errorf("export requires an HTTP(S) API server URL without embedded credentials")
	}
	server.Host = strings.ToLower(server.Host)
	server.Path = strings.TrimRight(server.Path, "/")
	address := server.String()
	if err := checkExportSource(cfg, directory, address); err != nil {
		return "", "", err
	}
	return directory, address, nil
}

// Resolve existing ancestors too, so a new destination and symlink aliases use
// the same binding once the directory exists.
func canonicalOutputDir(directory string) (string, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	ancestor := absolute
	for {
		resolved, err := filepath.EvalSymlinks(ancestor)
		if err == nil {
			relative, err := filepath.Rel(ancestor, absolute)
			if err != nil {
				return "", err
			}
			return filepath.Join(resolved, relative), nil
		}
		if !os.IsNotExist(err) || filepath.Dir(ancestor) == ancestor {
			return "", err
		}
		ancestor = filepath.Dir(ancestor)
	}
}

func checkExportSource(cfg *config.StoredConfig, directory, server string) error {
	if previous := cfg.ExportBindings[directory]; previous != "" && previous != server {
		return fmt.Errorf("export directory %q is bound to API server %q; current server is %q; use another output directory",
			directory, previous, server)
	}
	return nil
}

func bindExportSource(directory, server string) error {
	// Reload after fetching: authentication may have refreshed stored credentials.
	cfg, err := config.LoadStoredConfig()
	if err != nil {
		return fmt.Errorf("record export source: %w", err)
	}
	if err := checkExportSource(cfg, directory, server); err != nil {
		return err
	}
	if cfg.ExportBindings[directory] == server {
		return nil
	}
	if cfg.ExportBindings == nil {
		cfg.ExportBindings = make(map[string]string)
	}
	cfg.ExportBindings[directory] = server
	if err := config.SaveStoredConfig(cfg); err != nil {
		return fmt.Errorf("record export source: %w", err)
	}
	return nil
}
