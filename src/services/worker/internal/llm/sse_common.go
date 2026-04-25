package llm

import (
	"bufio"
	"context"
	"io"
	"strings"
)

func forEachSSEData(ctx context.Context, r io.Reader, markActivity func(), handle func(string) error) error {
	reader := bufio.NewReader(r)
	dataLines := []string{}
	type readResult struct {
		line string
		err  error
	}
	var closer io.Closer
	if c, ok := r.(io.Closer); ok {
		closer = c
	}
	for {
		if err := streamContextError(ctx, nil); err != nil {
			if closer != nil {
				_ = closer.Close()
			}
			return err
		}

		resultCh := make(chan readResult, 1)
		go func() {
			line, err := reader.ReadString('\n')
			resultCh <- readResult{line: line, err: err}
		}()

		var result readResult
		select {
		case <-ctx.Done():
			if closer != nil {
				_ = closer.Close()
			}
			return streamContextError(ctx, nil)
		case result = <-resultCh:
		}
		if result.err != nil && result.err != io.EOF {
			return streamContextError(ctx, result.err)
		}
		if len(result.line) > 0 && markActivity != nil {
			markActivity()
		}

		cleaned := strings.TrimRight(result.line, "\r\n")
		if cleaned == "" {
			if len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				dataLines = dataLines[:0]
				if err := handle(data); err != nil {
					return err
				}
			}
		} else if strings.HasPrefix(cleaned, ":") {
			// ignore
		} else if strings.HasPrefix(cleaned, "data:") {
			dataLines = append(dataLines, strings.TrimLeft(cleaned[len("data:"):], " "))
		}

		if result.err == io.EOF {
			break
		}
	}

	if len(dataLines) > 0 {
		if err := handle(strings.Join(dataLines, "\n")); err != nil {
			return err
		}
	}
	return nil
}
