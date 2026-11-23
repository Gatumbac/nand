package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/Gatumbac/nand"
	"github.com/Gatumbac/nand/provider/openai"
	"github.com/Gatumbac/nand/tools"
)

type cliOptions struct {
	baseURL            string
	apiKey             string
	model              string
	system             string
	maxTurns           int
	maxToolOutputBytes int
	maxContextChars    int
	tracePath          string
	showStats          bool
	pricing            nand.Pricing
	prompt             string
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) error {
	opts, err := parseOptions(args, getenv, stderr)
	if err != nil {
		return err
	}

	tracer, closeTrace, err := openTracer(opts.tracePath)
	if err != nil {
		return err
	}
	defer closeTrace()

	agent, err := buildAgent(opts, newTerminalApprover(stdin, stderr), tracer)
	if err != nil {
		return err
	}

	res, err := agent.Run(ctx, opts.prompt)
	if err != nil {
		return err
	}

	if opts.showStats {
		fmt.Fprintf(stderr, "turns=%d tokens(in=%d out=%d total=%d) cost=$%.6f\n",
			res.Turns, res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.TotalTokens, res.CostUSD)
	}

	_, err = fmt.Fprintln(stdout, res.Reply)
	return err
}

func buildAgent(opts cliOptions, approver nand.Approver, tracer nand.Tracer) (*nand.Agent, error) {
	client, err := openai.New(openai.Config{
		BaseURL: opts.baseURL,
		APIKey:  opts.apiKey,
		Model:   opts.model,
	})
	if err != nil {
		return nil, err
	}

	registry, err := nand.NewRegistry(
		tools.NewRead(),
		tools.NewWrite(),
		tools.NewEdit(),
		tools.NewBash(""),
	)
	if err != nil {
		return nil, err
	}

	return nand.NewAgent(nand.AgentConfig{
		Provider:           client,
		Registry:           registry,
		Approver:           approver,
		Tracer:             tracer,
		Pricing:            opts.pricing,
		System:             opts.system,
		MaxTurns:           opts.maxTurns,
		MaxToolOutputBytes: opts.maxToolOutputBytes,
		MaxContextChars:    opts.maxContextChars,
	})
}

func openTracer(path string) (nand.Tracer, func() error, error) {
	if strings.TrimSpace(path) == "" {
		return nil, func() error { return nil }, nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open trace file: %w", err)
	}
	return nand.NewJSONLTracer(f), f.Close, nil
}

func parseOptions(args []string, getenv func(string) string, stderr io.Writer) (cliOptions, error) {
	fs := flag.NewFlagSet("nand", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var opts cliOptions
	fs.StringVar(&opts.system, "system", getenv("NAND_SYSTEM_PROMPT"), "system prompt")
	fs.IntVar(&opts.maxTurns, "max-turns", 10, "maximum number of agent turns")
	fs.IntVar(&opts.maxToolOutputBytes, "max-output", 16*1024, "maximum tool output size in bytes")
	fs.IntVar(&opts.maxContextChars, "max-context", 120_000, "context compaction threshold in chars (0 disables)")
	fs.StringVar(&opts.tracePath, "trace", "", "write JSONL session trace to path")
	fs.BoolVar(&opts.showStats, "stats", false, "print turn, token, and cost summary to stderr")
	if err := fs.Parse(args); err != nil {
		return cliOptions{}, err
	}

	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		return cliOptions{}, errors.New("usage: nand [-system PROMPT] [-max-turns N] [-trace FILE] [-stats] <prompt>")
	}

	pricing, err := parsePricing(getenv)
	if err != nil {
		return cliOptions{}, err
	}

	opts.baseURL = getenv("NAND_BASE_URL")
	opts.apiKey = getenv("NAND_API_KEY")
	opts.model = getenv("NAND_MODEL")
	opts.pricing = pricing
	opts.prompt = prompt
	return opts, nil
}

func parsePricing(getenv func(string) string) (nand.Pricing, error) {
	inPrice, err := parseOptionalFloat(getenv("NAND_INPUT_PRICE_PER_M"), "NAND_INPUT_PRICE_PER_M")
	if err != nil {
		return nand.Pricing{}, err
	}
	outPrice, err := parseOptionalFloat(getenv("NAND_OUTPUT_PRICE_PER_M"), "NAND_OUTPUT_PRICE_PER_M")
	if err != nil {
		return nand.Pricing{}, err
	}
	return nand.Pricing{InputPerMillion: inPrice, OutputPerMillion: outPrice}, nil
}

func parseOptionalFloat(raw, name string) (float64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid %s: %q", name, raw)
	}
	return v, nil
}
