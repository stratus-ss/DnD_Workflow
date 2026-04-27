package perplexity

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"dnd-workflow/internal/config"

	"github.com/chromedp/chromedp"
)

type Browser struct {
	cfg         config.PerplexityConfig
	allocCancel context.CancelFunc
	ctxCancel   context.CancelFunc
	ctx         context.Context
}

func NewBrowser(cfg config.PerplexityConfig) *Browser {
	return &Browser{cfg: cfg}
}

func (b *Browser) Start() error {
	if err := os.MkdirAll(b.cfg.ChromeProfile, 0o755); err != nil {
		return fmt.Errorf("create profile dir: %w", err)
	}

	opts := b.buildAllocatorOpts()

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	b.allocCancel = allocCancel

	ctx, ctxCancel := chromedp.NewContext(allocCtx)
	b.ctxCancel = ctxCancel
	b.ctx = ctx

	return nil
}

func (b *Browser) buildAllocatorOpts() []chromedp.ExecAllocatorOption {
	width, height := parseWindowSize(b.cfg.WindowSize)
	return []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.UserDataDir(b.cfg.ChromeProfile),
		chromedp.Flag("headless", b.cfg.Headless),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("excludeSwitches", "enable-automation"),
		chromedp.Flag("disable-infobars", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.WindowSize(width, height),
	}
}

func parseWindowSize(s string) (int, int) {
	parts := strings.SplitN(s, ",", 2)
	if len(parts) == 2 {
		w, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		h, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 == nil && err2 == nil {
			return w, h
		}
	}
	return 1280, 900
}

func (b *Browser) Close() {
	if b.ctxCancel != nil {
		b.ctxCancel()
	}
	if b.allocCancel != nil {
		b.allocCancel()
	}
}

func (b *Browser) NavigateToSpace() error {
	sleepDur := time.Duration(b.cfg.PostNavigateSleepSec) * time.Second
	return chromedp.Run(b.ctx,
		chromedp.Navigate(b.cfg.SpaceURL),
		chromedp.WaitReady("body"),
		chromedp.Sleep(sleepDur),
	)
}

func (b *Browser) StartNewThread() error {
	newThreadJS := `
		(function() {
			const btns = document.querySelectorAll('button');
			for (const b of btns) {
				if (b.textContent.includes('New thread')) { b.click(); return true; }
			}
			return false;
		})()
	`
	var clicked bool
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(newThreadJS, &clicked)); err != nil {
		return fmt.Errorf("new thread: %w", err)
	}
	if !clicked {
		return fmt.Errorf("new thread button not found")
	}
	sleepDur := time.Duration(b.cfg.AfterNewThreadSleepSec) * time.Second
	return chromedp.Run(b.ctx, chromedp.Sleep(sleepDur))
}

func (b *Browser) UploadFile(filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("resolve absolute path: %w", err)
	}
	sel := b.cfg.Selectors.FileInput
	sleepDur := time.Duration(b.cfg.AfterUploadSleepSec) * time.Second
	return chromedp.Run(b.ctx,
		chromedp.SetUploadFiles(sel, []string{absPath}, chromedp.ByQuery),
		chromedp.Sleep(sleepDur),
	)
}

func (b *Browser) SubmitPrompt(prompt string) error {
	textSel := b.cfg.Selectors.TextInput
	pasteJS := fmt.Sprintf(`
		(function(text) {
			const el = document.querySelector(%s);
			if (!el) return 'no_textbox';
			el.focus();
			const dt = new DataTransfer();
			dt.setData('text/plain', text);
			const pe = new ClipboardEvent('paste', {
				bubbles: true, cancelable: true, clipboardData: dt
			});
			el.dispatchEvent(pe);
			if (el.textContent.length > 0) return 'paste:' + el.textContent.length;
			el.textContent = text;
			el.dispatchEvent(new InputEvent('input', {
				bubbles: true, inputType: 'insertText', data: text
			}));
			return 'fallback:' + el.textContent.length;
		})
	`, jsStringLiteral(textSel))

	var result string
	if err := chromedp.Run(b.ctx,
		chromedp.Evaluate(pasteJS+`(`+jsStringLiteral(prompt)+`)`, &result),
	); err != nil {
		return fmt.Errorf("insert prompt: %w", err)
	}
	slog.Info("text insert result", "result", result)
	if result == "no_textbox" {
		return fmt.Errorf("textbox not found")
	}

	submitSel := b.cfg.Selectors.SubmitButton
	return chromedp.Run(b.ctx,
		chromedp.Sleep(1*time.Second),
		chromedp.Click(submitSel, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
	)
}

func (b *Browser) WaitForResponse(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(b.ctx, timeout)
	defer cancel()
	return b.waitForCompletion(ctx)
}

func (b *Browser) waitForCompletion(ctx context.Context) error {
	pollInterval := time.Duration(b.cfg.ResponsePollIntervalSec) * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	stableTarget := b.cfg.ResponseStableCount
	var prevLen int
	stableCount := 0
	waitCount := 0

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for response")
		case <-ticker.C:
			waitCount++
			length, err := b.responseLength()
			if err != nil || length == 0 {
				if waitCount%6 == 0 {
					slog.Info("still waiting for response", "elapsed_sec", waitCount*b.cfg.ResponsePollIntervalSec)
				}
				continue
			}
			if length == prevLen {
				stableCount++
			} else {
				slog.Info("response growing", "chars", length)
				stableCount = 0
			}
			prevLen = length
			if stableCount >= stableTarget {
				return nil
			}
		}
	}
}

func (b *Browser) responseLength() (int, error) {
	responseSel := b.cfg.Selectors.ResponseArea
	responseJS := fmt.Sprintf(`
		(function() {
			const msgs = document.querySelectorAll(%s);
			if (msgs.length === 0) return 0;
			const last = msgs[msgs.length - 1];
			return last.textContent.length;
		})()
	`, jsStringLiteral(responseSel))
	var length int
	err := chromedp.Run(b.ctx, chromedp.Evaluate(responseJS, &length))
	return length, err
}

func (b *Browser) ExtractMarkdown() (string, error) {
	md, err := b.clipboardCopyMarkdown()
	if err == nil && len(md) > 100 {
		return md, nil
	}
	return b.htmlToMarkdownFallback()
}

func (b *Browser) clipboardCopyMarkdown() (string, error) {
	interceptJS := `
		window.__perplexityCopied = '';
		const origWrite = navigator.clipboard.writeText.bind(navigator.clipboard);
		navigator.clipboard.writeText = function(text) {
			window.__perplexityCopied = text;
			return origWrite(text);
		};
	`
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(interceptJS, nil)); err != nil {
		return "", fmt.Errorf("inject interceptor: %w", err)
	}

	copySel := b.cfg.Selectors.CopyButton
	clickJS := fmt.Sprintf(`
		(function() {
			const btns = document.querySelectorAll(%s + ', button[data-testid="copy-button"]');
			const last = btns[btns.length - 1];
			if (last) { last.click(); return true; }
			const allBtns = document.querySelectorAll('button');
			for (const b of allBtns) {
				if (b.textContent.trim() === 'Copy' || b.querySelector('svg path[d*="M16 1H4"]')) {
					b.click(); return true;
				}
			}
			return false;
		})()
	`, jsStringLiteral(copySel))
	var clicked bool
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(clickJS, &clicked)); err != nil {
		return "", fmt.Errorf("click copy: %w", err)
	}
	if !clicked {
		return "", fmt.Errorf("copy button not found")
	}

	time.Sleep(1 * time.Second)

	var md string
	readJS := `window.__perplexityCopied || ''`
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(readJS, &md)); err != nil {
		return "", fmt.Errorf("read clipboard: %w", err)
	}
	return md, nil
}

func (b *Browser) htmlToMarkdownFallback() (string, error) {
	responseSel := b.cfg.Selectors.ResponseArea
	extractJS := fmt.Sprintf(`
		(function() {
			const blocks = document.querySelectorAll(%s);
			let longest = '';
			for (const b of blocks) {
				if (b.innerHTML.length > longest.length) longest = b.innerHTML;
			}
			return longest;
		})()
	`, jsStringLiteral(responseSel))
	var html string
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(extractJS, &html)); err != nil {
		return "", fmt.Errorf("extract HTML: %w", err)
	}
	if html == "" {
		return "", fmt.Errorf("no response HTML found")
	}
	return convertHTMLToMarkdown(html)
}

func (b *Browser) ExtractPlainText() (string, error) {
	responseSel := b.cfg.Selectors.ResponseArea
	var text string
	if err := chromedp.Run(b.ctx,
		chromedp.TextContent(responseSel, &text, chromedp.ByQuery),
	); err != nil {
		return "", fmt.Errorf("extract text: %w", err)
	}
	return strings.TrimSpace(text), nil
}

func (b *Browser) TakeScreenshot(path string) error {
	var buf []byte
	if err := chromedp.Run(b.ctx, chromedp.CaptureScreenshot(&buf)); err != nil {
		return fmt.Errorf("screenshot: %w", err)
	}
	return os.WriteFile(path, buf, 0o644)
}

func (b *Browser) OpenThread(threadName string) error {
	openJS := `
		(function(name) {
			const links = document.querySelectorAll('a');
			for (const a of links) {
				if (a.textContent.trim().startsWith(name)) {
					a.click();
					return 'found';
				}
			}
			return 'not_found';
		})
	`
	var result string
	if err := chromedp.Run(b.ctx,
		chromedp.Evaluate(openJS+`(`+jsStringLiteral(threadName)+`)`, &result),
	); err != nil {
		return fmt.Errorf("open thread: %w", err)
	}
	if result == "not_found" {
		return fmt.Errorf("thread %q not found in sidebar", threadName)
	}
	slog.Info("opened thread", "name", threadName)
	sleepDur := time.Duration(b.cfg.AfterNewThreadSleepSec) * time.Second
	return chromedp.Run(b.ctx, chromedp.Sleep(sleepDur))
}

func (b *Browser) GenerateNotes(srtPath, promptText string) (string, string, error) {
	return b.GenerateNotesInThread(srtPath, promptText, "")
}

func (b *Browser) GenerateNotesInThread(srtPath, promptText, threadName string) (string, string, error) {
	if err := b.UploadAndSubmit(srtPath, promptText, threadName); err != nil {
		return "", "", err
	}

	timeout := time.Duration(b.cfg.ResponseTimeoutMin) * time.Minute
	if err := b.WaitForResponse(timeout); err != nil {
		return "", "", fmt.Errorf("wait: %w", err)
	}

	return b.ScrapeExistingResponse()
}

// UploadAndSubmit navigates to the Perplexity space, opens the thread, uploads the SRT
// file, and submits the prompt. It returns after submission without waiting for response.
// Caller is responsible for closing the browser via Close().
func (b *Browser) UploadAndSubmit(srtPath, promptText, threadName string) error {
	if err := b.NavigateToSpace(); err != nil {
		return fmt.Errorf("navigate: %w", err)
	}

	if !b.isOnPerplexitySpace() {
		return fmt.Errorf("not in Perplexity space, current URL: %s", b.currentURL())
	}

	if threadName != "" {
		if err := b.OpenThread(threadName); err != nil {
			slog.Warn("could not open thread, submitting in new thread", "error", err)
		}
	}

	if err := b.UploadFile(srtPath); err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	if err := b.SubmitPrompt(promptText); err != nil {
		return fmt.Errorf("submit: %w", err)
	}

	return nil
}

// ScrapeExistingResponse navigates to the Perplexity space and thread,
// then extracts the full markdown notes and narration summary from the last response.
func (b *Browser) ScrapeExistingResponse() (string, string, error) {
	if err := b.NavigateToSpace(); err != nil {
		return "", "", fmt.Errorf("navigate: %w", err)
	}

	if !b.isOnPerplexitySpace() {
		return "", "", fmt.Errorf("not in Perplexity space, current URL: %s", b.currentURL())
	}

	if b.cfg.ThreadName != "" {
		if err := b.OpenThread(b.cfg.ThreadName); err != nil {
			slog.Warn("could not open thread, scraping current page", "error", err)
		}
	}

	length, err := b.responseLength()
	if err != nil || length == 0 {
		return "", "", fmt.Errorf("no response visible on page")
	}

	fullMarkdown, err := b.ExtractMarkdown()
	if err != nil {
		return "", "", fmt.Errorf("extract markdown: %w", err)
	}

	summary := ParseSummary(fullMarkdown)
	if summary == "" {
		slog.Warn("ParseSummary returned empty - heading format may not match", "markdown_len", len(fullMarkdown))
	}
	return fullMarkdown, summary, nil
}

func (b *Browser) isOnPerplexitySpace() bool {
	url := b.currentURL()
	return strings.Contains(url, "/spaces/")
}

func (b *Browser) currentURL() string {
	var url string
	chromedp.Run(b.ctx, chromedp.Location(&url))
	return url
}

func ParseSummary(fullText string) string {
	re := regexp.MustCompile(`(?si)(?:^|\n)#[^\n]*?Session Narration[^\n]*\n(.*?)(?:\n#[^\n]*?DM Summary|\z)`)
	matches := re.FindStringSubmatch(fullText)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

func LoadPrompt(promptFile, sessionDate string) (string, error) {
	data, err := os.ReadFile(promptFile)
	if err != nil {
		return "", fmt.Errorf("read prompt file: %w", err)
	}
	text := strings.Replace(string(data), "{SESSION_DATE}", sessionDate, -1)
	return text, nil
}

func jsStringLiteral(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "$", "\\$")
	return "`" + s + "`"
}
