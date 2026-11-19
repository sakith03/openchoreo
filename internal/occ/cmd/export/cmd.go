// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package export

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/openchoreo/openchoreo/internal/occ/auth"
	"github.com/openchoreo/openchoreo/internal/occ/resources/client"
)

// NewExportCmd builds the command for exporting configuration as YAML files.
func NewExportCmd(f client.NewClientFunc) *cobra.Command {
	var params Params
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export OpenChoreo configuration as YAML manifests",
		Long: `Export supported OpenChoreo configuration across the cluster into a GitOps
directory tree, with one YAML manifest per resource. Requires unrestricted
read access to all supported resource kinds.

Re-export skips identical files, replaces changed files (including local edits),
and removes files for resources deleted from the cluster. Unrelated files are
preserved. Status and server metadata are removed from exported manifests.

Releases include the last three calendar months (UTC), binding-referenced
releases, and releases without creation timestamps. Use --all-releases for the
complete history. Existing older release files are preserved while their
releases remain in the cluster.

Shows export progress and a summary of file changes and release history.`,
		Example: `  occ export -o ./gitops-export
  occ export --output /path/to/gitops-repository
  occ export -o .
  occ export -o ./gitops-export --all-releases`,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			return params.validate()
		},
		PreRunE: auth.RequireLogin(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f()
			if err != nil {
				return err
			}
			params.ConfirmEmpty = func(count int) (bool, error) {
				input, ok := cmd.InOrStdin().(*os.File)
				if !ok || !term.IsTerminal(int(input.Fd())) {
					return false, fmt.Errorf("empty export would delete %d managed manifests; rerun in a terminal to confirm", count)
				}
				return readEmptyConfirmation(input, cmd.ErrOrStderr(), count)
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Exporting resources..."); err != nil {
				return err
			}
			result, err := Run(cmd.Context(), c, params)
			if err != nil {
				return err
			}
			return writeExportSummary(cmd.OutOrStdout(), result)
		},
	}
	cmd.Flags().StringVarP(&params.OutputDir, "output", "o", "", "Directory for exported YAML manifests (required)")
	cmd.Flags().BoolVar(&params.AllReleases, "all-releases", false, "Export all releases instead of recent and binding-referenced releases")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func writeExportSummary(output io.Writer, result *ExportResult) error {
	summary := fmt.Sprintf("\nExport completed\n  Output:      %s\n  Resources:   %d selected resource(s)\n\n"+
		"Files:\n  CREATED   UPDATED   UNCHANGED   DELETED\n  %-10d%-10d%-12d%d\n\nRelease history:\n",
		result.OutputDir, len(result.Files), result.Counts.Created, result.Counts.Updated, result.Counts.Unchanged, result.Counts.Deleted)
	if result.ReleaseCutoff == nil {
		summary += "  Policy:        All releases\n"
	} else {
		summary += fmt.Sprintf("  Policy:        Last 3 calendar months, plus binding-referenced releases\n"+
			"  Cutoff (UTC):  %s (inclusive)\n",
			result.ReleaseCutoff.UTC().Format(time.RFC3339Nano))
	}
	_, err := fmt.Fprint(output, summary)
	return err
}

func readEmptyConfirmation(input io.Reader, output io.Writer, count int) (bool, error) {
	if _, err := fmt.Fprintf(output, "The cluster export is empty. Delete %d managed manifests? [y/N]: ", count); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read empty-export confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
