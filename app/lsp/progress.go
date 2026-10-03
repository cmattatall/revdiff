package lsp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Progress struct {
	Server  string
	Message string
	Since   time.Time
}

type workProgress struct {
	Title      string
	Message    *string
	Percentage *int
}

type progressState struct {
	mu       sync.Mutex
	activity string
	since    time.Time
	work     map[string]workProgress
}

func (p *progressState) setActivity(activity string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.activity == "" && len(p.work) == 0 {
		p.since = time.Now()
	}
	p.activity = activity
}

func (p *progressState) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.work = nil
}

func (p *progressState) update(raw json.RawMessage) {
	var params struct {
		Token json.RawMessage
		Value struct {
			Kind string
			workProgress
		}
	}
	if json.Unmarshal(raw, &params) != nil || len(params.Token) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	token := string(params.Token)
	switch params.Value.Kind {
	case "begin":
		if p.activity == "" && len(p.work) == 0 {
			p.since = time.Now()
		}
		if p.work == nil {
			p.work = make(map[string]workProgress)
		}
		p.work[token] = params.Value.workProgress
	case "report":
		work, ok := p.work[token]
		if !ok {
			return
		}
		if params.Value.Message != nil {
			work.Message = params.Value.Message
		}
		if params.Value.Percentage != nil {
			work.Percentage = params.Value.Percentage
		}
		p.work[token] = work
	case "end":
		delete(p.work, token)
	}
}

func (p *progressState) snapshot(server string) Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	messages := make([]string, 0, len(p.work))
	for _, work := range p.work {
		message := work.Title
		if work.Message != nil && *work.Message != "" {
			message += ": " + *work.Message
		}
		if work.Percentage != nil {
			message += fmt.Sprintf(" (%d%%)", *work.Percentage)
		}
		messages = append(messages, message)
	}
	sort.Strings(messages)
	message := strings.Join(messages, "; ")
	if message == "" {
		message = p.activity
	}
	return Progress{Server: server, Message: message, Since: p.since}
}

func (c *Client) Progress() []Progress {
	c.mu.Lock()
	defer c.mu.Unlock()
	var progress []Progress
	for key, session := range c.sessions {
		if status := session.progress.snapshot(c.servers[key.server].Name); status.Message != "" {
			progress = append(progress, status)
		}
	}
	sort.Slice(progress, func(i, j int) bool { return progress[i].Server < progress[j].Server })
	return progress
}
