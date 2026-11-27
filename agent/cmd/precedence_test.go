package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pkg_flags "github.com/dengyie/mangohub/agent/cmd/flags"
	"github.com/spf13/cobra"
)

func TestConfigPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")

	fileCfg := pkg_flags.Config{
		Token:    "file_token",
		Endpoint: "https://file.endpoint.com",
		Interval: 10.0,
		RawICMP:  true,
	}
	fileBytes, err := json.Marshal(fileCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configFile, fileBytes, 0600); err != nil {
		t.Fatal(err)
	}

	// 1. Test CLI flag wins over ENV and Config File
	t.Run("CLI_Wins_Over_ENV_And_File", func(t *testing.T) {
		t.Setenv("AGENT_TOKEN", "env_token")
		t.Setenv("AGENT_ENDPOINT", "https://env.endpoint.com")

		cmd := &cobra.Command{}
		cmd.Flags().String("token", "", "")
		cmd.Flags().String("endpoint", "", "")
		cmd.Flags().Float64("interval", 3.0, "")
		cmd.Flags().Bool("raw-icmp", false, "")
		_ = cmd.Flags().Set("token", "cli_token") // explicitly set on CLI

		flags = &pkg_flags.Config{
			Token:      "cli_token",
			ConfigFile: configFile,
		}

		err := loadConfiguration(cmd)
		if err != nil {
			t.Fatal(err)
		}

		if flags.Token != "cli_token" {
			t.Fatalf("expected CLI flag token %q to win, got %q", "cli_token", flags.Token)
		}
		// Endpoint was NOT set on CLI, so ENV should win over file
		if flags.Endpoint != "https://env.endpoint.com" {
			t.Fatalf("expected ENV endpoint to win, got %q", flags.Endpoint)
		}
		// Interval was neither on CLI nor in ENV, so Config File should win
		if flags.Interval != 10.0 {
			t.Fatalf("expected Config File interval 10.0 to win, got %f", flags.Interval)
		}
		if !flags.RawICMP {
			t.Fatal("expected Config File raw_icmp to be enabled")
		}
	})

	// 2. Test ENV wins over Config File when CLI not passed
	t.Run("ENV_Wins_Over_File", func(t *testing.T) {
		t.Setenv("AGENT_TOKEN", "env_token_2")
		t.Setenv("AGENT_RAW_ICMP", "false")

		cmd := &cobra.Command{}
		cmd.Flags().String("token", "", "")
		cmd.Flags().Bool("raw-icmp", false, "")

		flags = &pkg_flags.Config{
			ConfigFile: configFile,
		}

		err := loadConfiguration(cmd)
		if err != nil {
			t.Fatal(err)
		}

		if flags.Token != "env_token_2" {
			t.Fatalf("expected ENV token %q to win over file, got %q", "env_token_2", flags.Token)
		}
		if flags.RawICMP {
			t.Fatal("expected AGENT_RAW_ICMP=false to override Config File")
		}
	})

	// 3. Test Config File wins over Defaults when neither CLI nor ENV is set
	t.Run("File_Wins_Over_Default", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().String("token", "", "")

		flags = &pkg_flags.Config{
			ConfigFile: configFile,
		}

		err := loadConfiguration(cmd)
		if err != nil {
			t.Fatal(err)
		}

		if flags.Token != "file_token" {
			t.Fatalf("expected file token %q to win, got %q", "file_token", flags.Token)
		}
	})
}
