/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cli

import (
	"log/slog"
	"os"

	"github.com/konflux-ci/operator-foundry/internal/cli/fbc"
	"github.com/konflux-ci/operator-foundry/internal/cli/testoutput"
	"github.com/spf13/cobra"
)

var logLevel string

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "operator-foundry",
		Short: "CLI for operator pipeline tasks",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			level := new(slog.LevelVar)
			switch logLevel {
			case "debug":
				level.Set(slog.LevelDebug)
			case "warn":
				level.Set(slog.LevelWarn)
			case "error":
				level.Set(slog.LevelError)
			default:
				level.Set(slog.LevelInfo)
			}
			handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
			slog.SetDefault(slog.New(handler))
			return nil
		},
	}

	root.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level: debug, info, warn, error")
	root.AddCommand(fbc.NewFBCCmd())
	root.AddCommand(testoutput.NewMakeResultJSONCmd())

	return root
}

func Execute() error {
	return NewRootCmd().Execute()
}
