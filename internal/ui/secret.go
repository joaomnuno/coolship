package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Ask reads one line with an optional default. Noninteractive input is an
// error: a prompt must never block a pipeline.
func (p *Prompter) Ask(ctx context.Context, prompt, fallback string) (string, error) {
	if !p.streams.Interactive {
		return "", errors.New(prompt + " must be given as a flag when input is noninteractive")
	}
	if fallback != "" {
		prompt += " [" + fallback + "]"
	}
	if _, err := fmt.Fprint(p.streams.Err, prompt+": "); err != nil {
		return "", err
	}
	answer, err := p.readLine(ctx)
	if err != nil {
		return "", err
	}
	if answer == "" {
		return fallback, nil
	}
	return answer, nil
}

// AskSecret reads a token without echoing it when stdin is a terminal, and
// reads one line otherwise (for --token-stdin style piping).
func (p *Prompter) AskSecret(ctx context.Context, prompt string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if file, ok := p.streams.In.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		if _, err := fmt.Fprint(p.streams.Err, prompt+": "); err != nil {
			return "", err
		}
		secret, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(p.streams.Err)
		if err != nil {
			return "", fmt.Errorf("read token: %w", err)
		}
		return strings.TrimSpace(string(secret)), nil
	}
	return readSecretLine(p.input)
}

// ReadSecretLine reads a token from a non-terminal reader such as a pipe.
func ReadSecretLine(reader io.Reader) (string, error) {
	return readSecretLine(bufio.NewReader(reader))
}

func readSecretLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read token: %w", err)
	}
	return strings.TrimSpace(line), nil
}
