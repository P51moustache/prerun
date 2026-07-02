package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/term"
)

const workdir = "/workspace"

type RunOptions struct {
	DefaultImage string
	ArtifactDir  string // host dir where job artifacts are stored between jobs
	Breakpoint   string // "job" or "job:stepN" (1-based); pause before that step
	BreakExec    string // instead of an interactive shell, run this command at the breakpoint (scripting/tests)
	Docker       string // docker binary (or podman)
}

type Runner struct {
	p    *Pipeline
	opts RunOptions

	mu         sync.Mutex
	currentCID string
}

func NewRunner(p *Pipeline, opts RunOptions) *Runner {
	return &Runner{p: p, opts: opts}
}

func (r *Runner) validateBreakpoint() error {
	spec := r.opts.Breakpoint
	if spec == "" {
		return nil
	}
	name, step := parseBreakpoint(spec)
	job := r.p.job(name)
	if job == nil {
		return fmt.Errorf("--break %q: no job named %q in this pipeline", spec, name)
	}
	if step < 1 || step > len(job.Script) {
		return fmt.Errorf("--break %q: job %q has %d step(s); step must be 1..%d", spec, name, len(job.Script), len(job.Script))
	}
	return nil
}

// setCurrent tracks the running container so the signal handler can clean it
// up — without this, Ctrl-C during a long step would leak the container.
func (r *Runner) setCurrent(cid string) {
	r.mu.Lock()
	r.currentCID = cid
	r.mu.Unlock()
}

func (r *Runner) Run() error {
	if err := r.validateBreakpoint(); err != nil {
		return err
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	go func() {
		s, ok := <-sig
		if !ok {
			return
		}
		r.mu.Lock()
		cid := r.currentCID
		r.mu.Unlock()
		if cid != "" {
			exec.Command(r.opts.Docker, "rm", "-f", cid).Run()
		}
		fmt.Fprintf(os.Stderr, "\n\x1b[33m⏹ interrupted (%v) — container cleaned up\x1b[0m\n", s)
		os.Exit(130)
	}()

	fmt.Printf("→ %d stage(s), %d job(s)\n", len(r.usedStages()), len(r.p.Jobs))
	for _, job := range r.p.Jobs {
		if err := r.runJob(job); err != nil {
			return fmt.Errorf("job %q failed: %w", job.Name, err)
		}
	}
	fmt.Printf("\x1b[32m✓ pipeline passed\x1b[0m\n")
	return nil
}

func (r *Runner) usedStages() []string {
	seen := map[string]bool{}
	var out []string
	for _, j := range r.p.Jobs {
		if !seen[j.Stage] {
			seen[j.Stage] = true
			out = append(out, j.Stage)
		}
	}
	return out
}

func sanitize(name string) string {
	s := strings.NewReplacer(":", "__", "/", "__", "\\", "__", " ", "_").Replace(name)
	// Never let a job name become a path component that escapes the store.
	if s == "" || strings.Trim(s, ".") == "" {
		return "job_" + s
	}
	return s
}

func (r *Runner) docker(args ...string) *exec.Cmd {
	return exec.Command(r.opts.Docker, args...)
}

func (r *Runner) runJob(job *Job) error {
	image := job.Image
	if image == "" {
		image = r.opts.DefaultImage
	}
	fmt.Printf("\n\x1b[1m[%s]\x1b[0m stage=%s image=%s\n", job.Name, job.Stage, image)

	// Start an idle container we exec steps into — this is what makes
	// step-level control (and breakpoints) possible.
	out, err := r.docker("run", "-d", "--entrypoint", "", image, "sleep", "86400").Output()
	if err != nil {
		return fmt.Errorf("starting container from %s: %w (is your container runtime up, and the image available locally or pullable?)", image, stderrOf(err))
	}
	cid := strings.TrimSpace(string(out))
	r.setCurrent(cid)
	defer func() {
		r.setCurrent("")
		r.docker("rm", "-f", cid).Run()
	}()

	if err := r.docker("exec", cid, "mkdir", "-p", workdir).Run(); err != nil {
		return fmt.Errorf("preparing %s: %w", workdir, err)
	}

	// Inject artifacts from upstream jobs (needs > dependencies > all earlier stages).
	for _, dep := range r.p.artifactSources(job) {
		src := filepath.Join(r.opts.ArtifactDir, sanitize(dep))
		if _, err := os.Stat(src); err != nil {
			continue // upstream job declared no artifacts or produced none
		}
		if err := r.docker("cp", src+string(filepath.Separator)+".", cid+":"+workdir).Run(); err != nil {
			return fmt.Errorf("injecting artifacts from %q: %w", dep, err)
		}
		fmt.Printf("  \x1b[2m↳ artifacts from %s injected\x1b[0m\n", dep)
	}

	for i, step := range job.Script {
		if r.breakpointHits(job.Name, i+1) {
			if err := r.pause(job, cid, i+1); err != nil {
				return err
			}
		}
		fmt.Printf("  \x1b[36m$ %s\x1b[0m\n", step)
		if err := r.execStep(cid, step); err != nil {
			return fmt.Errorf("step %d (`%s`): %w", i+1, step, err)
		}
	}

	// Collect declared artifacts to the host store.
	if len(job.Artifacts.Paths) > 0 {
		dst := filepath.Join(r.opts.ArtifactDir, sanitize(job.Name))
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		for _, p := range job.Artifacts.Paths {
			clean := strings.TrimSuffix(strings.TrimSpace(p), "/")
			if clean == "" || strings.HasPrefix(clean, "/") || strings.Contains(clean, "..") {
				return fmt.Errorf("artifact path %q must be relative to %s", p, workdir)
			}
			if err := r.docker("cp", cid+":"+workdir+"/"+clean, dst).Run(); err != nil {
				return fmt.Errorf("collecting artifact %q (did the job create it?): %w", p, err)
			}
		}
		fmt.Printf("  \x1b[2m↳ artifacts stored: %s\x1b[0m\n", strings.Join(job.Artifacts.Paths, ", "))
	}
	fmt.Printf("  \x1b[32m✓ %s\x1b[0m\n", job.Name)
	return nil
}

func (r *Runner) execStep(cid, step string) error {
	cmd := r.docker("exec", "-w", workdir, cid, "/bin/sh", "-c", step)
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		fmt.Printf("  %s\n", sc.Text())
	}
	scanErr := sc.Err()
	waitErr := cmd.Wait()
	if waitErr != nil {
		return waitErr
	}
	if scanErr != nil {
		// Truncated output must never be mistaken for complete, successful output.
		return fmt.Errorf("reading step output: %w", scanErr)
	}
	return nil
}

// parseBreakpoint: spec is "job" (break before step 1) or "job:N" (before
// step N, 1-based). Job names may themselves contain ":".
func parseBreakpoint(spec string) (string, int) {
	if i := strings.LastIndex(spec, ":"); i > 0 {
		if n, err := parseInt(spec[i+1:]); err == nil {
			return spec[:i], n
		}
	}
	return spec, 1
}

func (r *Runner) breakpointHits(jobName string, step int) bool {
	if r.opts.Breakpoint == "" {
		return false
	}
	name, stepSpec := parseBreakpoint(r.opts.Breakpoint)
	return name == jobName && step == stepSpec
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

func (r *Runner) pause(job *Job, cid string, step int) error {
	fmt.Printf("  \x1b[33m⏸ breakpoint: %s before step %d\x1b[0m\n", job.Name, step)
	if r.opts.BreakExec != "" {
		fmt.Printf("  \x1b[33m⏸ running --break-exec\x1b[0m\n")
		return r.execStep(cid, r.opts.BreakExec)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Printf("  \x1b[33m⏸ (no TTY — attach manually: %s exec -it -w %s %s /bin/sh)\x1b[0m\n", r.opts.Docker, workdir, cid)
		return nil
	}
	fmt.Printf("  \x1b[33m⏸ opening shell inside the job container — exit to resume the pipeline\x1b[0m\n")
	sh := r.docker("exec", "-it", "-w", workdir, cid, "/bin/sh")
	sh.Stdin, sh.Stdout, sh.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := sh.Run(); err != nil {
		fmt.Printf("  \x1b[33m⏸ shell exited (%v) — resuming\x1b[0m\n", err)
	}
	return nil
}

func stderrOf(err error) error {
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		return fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
	}
	return err
}
