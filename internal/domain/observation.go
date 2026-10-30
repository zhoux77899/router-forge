package domain

import (
	"encoding/json"
	"errors"
)

func (s *RunSnapshot) ApplyObservation(o ExecutionObservation) error {
	if err := s.ValidateSources(); err != nil {
		return err
	}
	if err := o.Validate(); err != nil {
		return err
	}
	switch o.Kind {
	case "tools":
		if s.Runtime != nil {
			return errors.New("execution tools have already been observed")
		}
		s.Runtime = &RuntimeSnapshot{Tools: append([]ToolStatus{}, o.Tools...), ObservedAt: o.Timestamp, WorkDir: o.WorkDir, DockerGateway: o.DockerGateway}
		for _, tool := range o.Tools {
			switch tool.Name {
			case "harbor":
				s.HarborVersion = tool.Version
			case "uv":
				s.UVVersion = tool.Version
			}
		}
	case "task":
		if s.Runtime == nil || s.Runtime.TaskPath != "" {
			return errors.New("task observation requires recorded tools and cannot replace an existing task")
		}
		for _, tool := range s.Runtime.Tools {
			if (tool.Name == "uv" || tool.Name == "harbor") && !tool.Available {
				return errors.New("task materialization requires available evaluation tools")
			}
		}
		if *o.Task != s.ResolvedTasks[0] {
			return errors.New("materialized task source differs from the approved task")
		}
		s.Runtime.RegistryURL, s.Runtime.TaskPath, s.Runtime.TaskName, s.Runtime.TrialPrefix = o.RegistryURL, o.TaskPath, o.TaskName, o.TrialPrefix
	case "agent":
		if s.Runtime == nil || s.Runtime.TaskPath == "" || s.Runtime.AgentVersion != "" {
			return errors.New("independent agent observation requires recorded task materialization and cannot replace an existing observation")
		}
		s.Runtime.AgentVersion, s.Runtime.AgentVersionOutput, s.Runtime.AgentObservedAt = o.AgentVersion, o.AgentVersionOutput, o.Timestamp
	}
	hash, err := s.ApprovalHash()
	if err != nil || hash != s.ConfigHash {
		return errors.New("execution observation changed the approved configuration")
	}
	return nil
}

func CloneSnapshot(s RunSnapshot) (RunSnapshot, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return RunSnapshot{}, err
	}
	var copy RunSnapshot
	err = json.Unmarshal(b, &copy)
	return copy, err
}
