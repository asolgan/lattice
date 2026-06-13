// Package weaver implements the lattice weaver command group: operator
// list/disable/enable/revoke controls for Weaver convergence targets (FR30),
// via the lattice.ctrl.weaver.* NATS Services control plane.
package weaver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/asolgan/lattice/cmd/lattice/output"
	"github.com/asolgan/lattice/internal/weaver/control"
)

// validateTargetID rejects a targetId that is empty or contains a "." before
// the request is published. The control subject is
// lattice.ctrl.weaver.<targetId>.<op> and the endpoints subscribe a
// single-token wildcard for <targetId>, so a dotted (or empty) targetId builds
// a subject no endpoint matches — the request would otherwise hang to the
// client timeout with an opaque "no responders" rather than a clear error.
// Registered target ids are dot-free single tokens (install-validated), so this
// mirrors the server-side targetId shape.
func validateTargetID(targetID string) error {
	if targetID == "" {
		return fmt.Errorf("targetId must not be empty")
	}
	if strings.Contains(targetID, ".") {
		return fmt.Errorf("targetId %q must not contain '.' (a registered targetId is a single dot-free token)", targetID)
	}
	return nil
}

// NewCommand returns the cobra.Command for the weaver command group.
func NewCommand(natsURL, outputFmt *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "weaver",
		Short: "Operate Weaver convergence targets (list/disable/enable/revoke)",
	}
	cmd.AddCommand(newListCommand(natsURL, outputFmt))
	cmd.AddCommand(newDisableCommand(natsURL, outputFmt))
	cmd.AddCommand(newEnableCommand(natsURL, outputFmt))
	cmd.AddCommand(newRevokeCommand(natsURL, outputFmt))
	return cmd
}

// request sends a control-plane request to subject and decodes the
// control.ControlResponse. Connection is via output.Connect's raw *nats.Conn
// (conn.NATS()) since the weaver-control endpoints are plain NATS Services
// responders, not JetStream.
func request(natsURL, subject string) (control.ControlResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), output.DefaultTimeout)
	defer cancel()

	conn, err := output.Connect(ctx, natsURL)
	if err != nil {
		return control.ControlResponse{}, err
	}
	defer conn.Close()

	reply, err := conn.NATS().RequestWithContext(ctx, subject, nil)
	if err != nil {
		return control.ControlResponse{}, fmt.Errorf("request %s: %w", subject, err)
	}

	var resp control.ControlResponse
	if err := json.Unmarshal(reply.Data, &resp); err != nil {
		return control.ControlResponse{}, fmt.Errorf("decode response from %s: %w", subject, err)
	}
	if resp.Error != "" {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}

func newListCommand(natsURL, outputFmt *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered Weaver convergence targets",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := request(*natsURL, control.ListSubject())
			if err != nil {
				if *outputFmt == "json" {
					return output.PrintJSONError("ControlError", err.Error())
				}
				return err
			}

			if *outputFmt == "json" {
				return output.PrintJSON(resp.Targets)
			}
			if len(resp.Targets) == 0 {
				fmt.Println("(no registered targets)")
				return nil
			}
			fmt.Printf("%-20s %-30s %-10s %s\n", "TARGET_ID", "LENS_REF", "STATE", "GAPS")
			for _, t := range resp.Targets {
				fmt.Printf("%-20s %-30s %-10s %v\n", t.TargetID, t.LensRef, t.State, t.Gaps)
			}
			return nil
		},
	}
}

func newDisableCommand(natsURL, outputFmt *string) *cobra.Command {
	return &cobra.Command{
		Use:   "disable <targetId>",
		Short: "Disable a Weaver convergence target (pause dispatch)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetID := args[0]
			if err := validateTargetID(targetID); err != nil {
				if *outputFmt == "json" {
					return output.PrintJSONError("ControlError", err.Error())
				}
				return err
			}
			resp, err := request(*natsURL, control.TargetSubject(targetID, "disable"))
			if err != nil {
				if *outputFmt == "json" {
					return output.PrintJSONError("ControlError", err.Error())
				}
				return err
			}

			if *outputFmt == "json" {
				return output.PrintJSON(resp.Disable)
			}
			fmt.Printf("target %q disabled\n", targetID)
			return nil
		},
	}
}

func newEnableCommand(natsURL, outputFmt *string) *cobra.Command {
	return &cobra.Command{
		Use:   "enable <targetId>",
		Short: "Enable a Weaver convergence target (resume dispatch)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetID := args[0]
			if err := validateTargetID(targetID); err != nil {
				if *outputFmt == "json" {
					return output.PrintJSONError("ControlError", err.Error())
				}
				return err
			}
			resp, err := request(*natsURL, control.TargetSubject(targetID, "enable"))
			if err != nil {
				if *outputFmt == "json" {
					return output.PrintJSONError("ControlError", err.Error())
				}
				return err
			}

			if *outputFmt == "json" {
				return output.PrintJSON(resp.Enable)
			}
			fmt.Printf("target %q enabled\n", targetID)
			return nil
		},
	}
}

func newRevokeCommand(natsURL, outputFmt *string) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <targetId>",
		Short: "Revoke a Weaver convergence target (remove durable + in-flight marks; stays disabled)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetID := args[0]
			if err := validateTargetID(targetID); err != nil {
				if *outputFmt == "json" {
					return output.PrintJSONError("ControlError", err.Error())
				}
				return err
			}
			resp, err := request(*natsURL, control.TargetSubject(targetID, "revoke"))
			if err != nil {
				if *outputFmt == "json" {
					return output.PrintJSONError("ControlError", err.Error())
				}
				return err
			}

			if *outputFmt == "json" {
				return output.PrintJSON(resp.Revoke)
			}
			fmt.Printf("target %q revoked\n", targetID)
			return nil
		},
	}
}
