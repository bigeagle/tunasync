package worker

import (
	"errors"
	"io/ioutil"
	"strings"
	"time"

	"github.com/tuna/tunasync/internal"
)

type rsyncConfig struct {
	name                                         string
	rsyncCmd                                     string
	upstreamURL, username, password, excludeFile string
	extraOptions                                 []string
	workingDir, logDir, logFile                  string
	useIPv6, useIPv4                             bool
	interval                                     time.Duration
	retry                                        int
}

// An RsyncProvider provides the implementation to rsync-based syncing jobs
type rsyncProvider struct {
	baseProvider
	rsyncConfig
	options  []string
	dataSize string
}

func newRsyncProvider(c rsyncConfig) (*rsyncProvider, error) {
	// TODO: check config options
	if !strings.HasSuffix(c.upstreamURL, "/") {
		return nil, errors.New("rsync upstream URL should ends with /")
	}
	if c.retry == 0 {
		c.retry = defaultMaxRetry
	}
	provider := &rsyncProvider{
		baseProvider: baseProvider{
			name:     c.name,
			ctx:      NewContext(),
			interval: c.interval,
			retry:    c.retry,
		},
		rsyncConfig: c,
	}

	if c.rsyncCmd == "" {
		provider.rsyncCmd = "rsync"
	}

	options := []string{
		"-aHvh", "--no-o", "--no-g", "--stats",
		"--exclude", ".~tmp~/",
		"--delete", "--delete-after", "--delay-updates",
		"--safe-links", "--timeout=120", "--contimeout=120",
	}

	if c.useIPv6 {
		options = append(options, "-6")
	} else if c.useIPv4 {
		options = append(options, "-4")
	}

	if c.excludeFile != "" {
		options = append(options, "--exclude-from", c.excludeFile)
	}
	if c.extraOptions != nil {
		options = append(options, c.extraOptions...)
	}
	provider.options = options

	provider.ctx.Set(_WorkingDirKey, c.workingDir)
	provider.ctx.Set(_LogDirKey, c.logDir)
	provider.ctx.Set(_LogFileKey, c.logFile)

	return provider, nil
}

func (p *rsyncProvider) Type() providerEnum {
	return provRsync
}

func (p *rsyncProvider) Upstream() string {
	return p.upstreamURL
}

func (p *rsyncProvider) DataSize() string {
	return p.dataSize
}

func (p *rsyncProvider) Run() error {
	p.dataSize = ""
	if err := p.Start(); err != nil {
		return err
	}
	if err := p.Wait(); err != nil {
		return err
	}
	if logContent, err := ioutil.ReadFile(p.LogFile()); err == nil {
		p.dataSize = internal.ExtractSizeFromRsyncLog(logContent)
	}
	return nil
}

func (p *rsyncProvider) Start() error {
	p.Lock()
	defer p.Unlock()

	if p.IsRunning() {
		return errors.New("provider is currently running")
	}

	env := map[string]string{}
	if p.username != "" {
		env["USER"] = p.username
	}
	if p.password != "" {
		env["RSYNC_PASSWORD"] = p.password
	}
	command := []string{p.rsyncCmd}
	command = append(command, p.options...)
	command = append(command, p.upstreamURL, p.WorkingDir())

	p.cmd = newCmdJob(p, command, p.WorkingDir(), env)
	if err := p.prepareLogFile(false); err != nil {
		return err
	}

	if err := p.cmd.Start(); err != nil {
		return err
	}
	p.isRunning.Store(true)
	return nil
}
