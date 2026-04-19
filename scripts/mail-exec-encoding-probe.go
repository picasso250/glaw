package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unicode/utf8"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Println("usage: go run ./scripts/mail-exec-encoding-probe.go <script-path>")
		os.Exit(2)
	}

	scriptPath, err := filepath.Abs(os.Args[1])
	if err != nil {
		fmt.Printf("abs path failed: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch filepath.Ext(scriptPath) {
	case ".ps1":
		cmd = exec.CommandContext(ctx, "pwsh", "-File", scriptPath)
	case ".py":
		cmd = exec.CommandContext(ctx, "python", scriptPath)
	default:
		fmt.Printf("unsupported extension: %s\n", filepath.Ext(scriptPath))
		os.Exit(2)
	}

	cmd.Dir = filepath.Dir(scriptPath)
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err = cmd.Run()

	stdoutBytes := stdoutBuf.Bytes()
	stderrBytes := stderrBuf.Bytes()

	fmt.Printf("script=%s\n", scriptPath)
	fmt.Printf("run_err=%v\n", err)
	fmt.Printf("stdout_len=%d utf8=%v\n", len(stdoutBytes), utf8.Valid(stdoutBytes))
	fmt.Printf("stderr_len=%d utf8=%v\n", len(stderrBytes), utf8.Valid(stderrBytes))
	fmt.Println("stdout_text_begin")
	fmt.Print(stdoutBuf.String())
	fmt.Println("stdout_text_end")
	fmt.Println("stderr_text_begin")
	fmt.Print(stderrBuf.String())
	fmt.Println("stderr_text_end")

	if err := os.WriteFile(filepath.Join(filepath.Dir(scriptPath), "probe.stdout.bin"), stdoutBytes, 0644); err != nil {
		fmt.Printf("write stdout failed: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(scriptPath), "probe.stderr.bin"), stderrBytes, 0644); err != nil {
		fmt.Printf("write stderr failed: %v\n", err)
		os.Exit(1)
	}
}
