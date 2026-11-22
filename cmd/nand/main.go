package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Gatumbac/nand"
	"github.com/Gatumbac/nand/provider/openai"
	"github.com/Gatumbac/nand/tools"
)

type cliOptions struct {
	baseURL  string
	apiKey   string
	model    string
	system   string
	maxTurns int
	prompt   string
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	opts, err := parseOptions(args, getenv, stderr)
	if err != nil {
		return err
	}

	agent, err := buildAgent(opts)
	if err != nil {
		return err
	}

	res, err := agent.Run(ctx, opts.prompt)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(stdout, res.Reply)
	return err
}

func buildAgent(opts cliOptions) (*nand.Agent, error) {
	client, err := openai.New(openai.Config{
		BaseURL: opts.baseURL,
		APIKey:  opts.apiKey,
		Model:   opts.model,
	})
	if err != nil {
		return nil, err
	}

	registry, err := nand.NewRegistry(tools.NewRead())
	if err != nil {
		return nil, err
	}

	return nand.NewAgent(nand.AgentConfig{
		Provider: client,
		Registry: registry,
		System:   opts.system,
		MaxTurns: opts.maxTurns,
	})
}

func parseOptions(args []string, getenv func(string) string, stderr io.Writer) (cliOptions, error) {
	fs := flag.NewFlagSet("nand", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var system string
	var maxTurns int
	fs.StringVar(&system, "system", getenv("NAND_SYSTEM_PROMPT"), "system prompt")
	fs.IntVar(&maxTurns, "max-turns", 10, "maximum number of agent turns")
	if err := fs.Parse(args); err != nil {
		return cliOptions{}, err
	}

	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		return cliOptions{}, errors.New("usage: nand [-system PROMPT] [-max-turns N] <prompt>")
	}

	return cliOptions{
		baseURL:  getenv("NAND_BASE_URL"),
		apiKey:   getenv("NAND_API_KEY"),
		model:    getenv("NAND_MODEL"),
		system:   system,
		maxTurns: maxTurns,
		prompt:   prompt,
	}, nil
}
