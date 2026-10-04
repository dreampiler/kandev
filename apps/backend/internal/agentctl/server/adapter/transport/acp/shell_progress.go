package acp

import "github.com/kandev/kandev/internal/agentctl/types/streams"

func observeShellOutputProgress(shell *streams.ShellExecPayload, meta map[string]any, contents []streams.ToolCallContentItem, rawOutput any) {
	delta, hasDelta := terminalOutputData(meta, "terminal_output_delta")
	terminal, hasTerminal := terminalOutputData(meta, "terminal_output")
	content, hasContent := cumulativeShellContent(contents)
	if !hasDelta && !hasTerminal && !hasContent && rawOutput == nil {
		return
	}
	output := ensureShellOutput(shell)
	var added uint64
	if hasDelta {
		added = uint64(len(delta))
	}
	if hasTerminal {
		added = max(added, cumulativeOutputGrowth(&output.ObservedTerminalBytes, uint64(len(terminal))))
	}
	if hasContent {
		added = max(added, cumulativeOutputGrowth(&output.ObservedContentBytes, uint64(len(content))))
	}
	if rawOutput != nil {
		added = max(added, cumulativeOutputGrowth(&output.ObservedResultBytes, shellResultBytes(rawOutput)))
	}
	output.ProgressBytes += added
}

func cumulativeOutputGrowth(previous *uint64, size uint64) uint64 {
	old := *previous
	*previous = size
	if size > old {
		return size - old
	}
	return 0
}

func shellResultBytes(result any) uint64 {
	switch value := unwrapShellRawOutput(result).(type) {
	case string:
		return uint64(len(value))
	case map[string]any:
		stdout, hasStdout := value["stdout"].(string)
		stderr, hasStderr := value["stderr"].(string)
		if hasStdout || hasStderr {
			return uint64(len(stdout) + len(stderr))
		}
		if text, ok := value["output"].(string); ok {
			return uint64(len(text))
		}
		if text, ok := value["formatted_output"].(string); ok {
			return uint64(len(text))
		}
	}
	return 0
}
