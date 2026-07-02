package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// StringList accepts both YAML forms GitLab allows for script:
//
//	script: echo hi
//	script: [echo hi, echo bye]
type StringList []string

func (s *StringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var one string
		if err := node.Decode(&one); err != nil {
			return err
		}
		*s = []string{one}
		return nil
	case yaml.SequenceNode:
		var many []string
		if err := node.Decode(&many); err != nil {
			return err
		}
		*s = many
		return nil
	}
	return fmt.Errorf("script: expected string or list")
}

type Artifacts struct {
	Paths []string `yaml:"paths"`
}

// Need accepts both `needs: [jobname]` and `needs: [{job: jobname}]`.
type Need struct {
	Job string
}

func (n *Need) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		return node.Decode(&n.Job)
	}
	var obj struct {
		Job string `yaml:"job"`
	}
	if err := node.Decode(&obj); err != nil {
		return err
	}
	n.Job = obj.Job
	return nil
}

type Job struct {
	Name         string     `yaml:"-"`
	Stage        string     `yaml:"stage"`
	Image        string     `yaml:"image"`
	Script       StringList `yaml:"script"`
	Artifacts    Artifacts  `yaml:"artifacts"`
	Needs        []Need     `yaml:"needs"`
	Dependencies []string   `yaml:"dependencies"`
}

type Pipeline struct {
	Stages []string
	Jobs   []*Job // in stage order, then file order within a stage
}

// Reserved top-level GitLab CI keys that are not jobs. Keys we don't
// implement are rejected loudly in parse() rather than silently ignored.
var reservedKeys = map[string]bool{
	"stages": true, "variables": true, "default": true, "workflow": true,
	"include": true, "image": true, "services": true, "before_script": true,
	"after_script": true, "cache": true, "pages": true,
}

var defaultStages = []string{".pre", "build", "test", "deploy", ".post"}

func parsePipeline(path string) (*Pipeline, []string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("%s: expected a YAML mapping at top level", path)
	}
	root := doc.Content[0]

	p := &Pipeline{Stages: defaultStages}
	var unsupported []string
	jobOrder := map[string]int{}

	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i].Value
		val := root.Content[i+1]

		if key == "stages" {
			var stages []string
			if err := val.Decode(&stages); err != nil {
				return nil, nil, fmt.Errorf("stages: %w", err)
			}
			p.Stages = stages
			continue
		}
		if reservedKeys[key] || strings.HasPrefix(key, ".") {
			if key != "stages" && !strings.HasPrefix(key, ".") {
				unsupported = append(unsupported, key)
			}
			continue
		}
		job := &Job{Name: key, Stage: "test"}
		if err := val.Decode(job); err != nil {
			return nil, nil, fmt.Errorf("job %q: %w", key, err)
		}
		if len(job.Script) == 0 {
			return nil, nil, fmt.Errorf("job %q: no script (only script-based jobs are supported)", key)
		}
		jobOrder[key] = i
		p.Jobs = append(p.Jobs, job)
	}

	if len(p.Jobs) == 0 {
		return nil, nil, fmt.Errorf("%s: no jobs found", path)
	}

	stageIndex := map[string]int{}
	for i, s := range p.Stages {
		stageIndex[s] = i
	}
	for _, j := range p.Jobs {
		if _, ok := stageIndex[j.Stage]; !ok {
			return nil, nil, fmt.Errorf("job %q uses stage %q which is not in stages: %v", j.Name, j.Stage, p.Stages)
		}
	}
	sort.SliceStable(p.Jobs, func(a, b int) bool {
		sa, sb := stageIndex[p.Jobs[a].Stage], stageIndex[p.Jobs[b].Stage]
		if sa != sb {
			return sa < sb
		}
		return jobOrder[p.Jobs[a].Name] < jobOrder[p.Jobs[b].Name]
	})
	return p, unsupported, nil
}

// artifactSources returns which prior jobs' artifacts to inject into job j.
// GitLab semantics (subset): `needs` wins if set; else `dependencies` if set;
// else all artifact-producing jobs from earlier stages.
func (p *Pipeline) artifactSources(j *Job) []string {
	if len(j.Needs) > 0 {
		var out []string
		for _, n := range j.Needs {
			out = append(out, n.Job)
		}
		return out
	}
	if j.Dependencies != nil {
		return j.Dependencies
	}
	stageIndex := map[string]int{}
	for i, s := range p.Stages {
		stageIndex[s] = i
	}
	var out []string
	for _, prev := range p.Jobs {
		if stageIndex[prev.Stage] < stageIndex[j.Stage] && len(prev.Artifacts.Paths) > 0 {
			out = append(out, prev.Name)
		}
	}
	return out
}
