package parser

import (
	"errors"
	"fmt"
	"gitlab.com/shar-workflow/shar/common/expression"
	"gitlab.com/shar-workflow/shar/model"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
	"regexp"
)

var (
	errMissingID                    = errors.New("missing id")
	errMissingServiceTaskDefinition = errors.New("missing service task definition")
)

func validModel(workflow *model.Workflow) error {
	// Iterate the processes
	for _, i := range workflow.Process {
		// Check the name
		if err := validName(i.Name); err != nil {
			return err
		}
		// Iterate through the elements
		for _, j := range i.Elements {
			if j.Id == "" {
				return &valError{Err: errMissingID, Context: j.Name}
			}
			switch j.Type {
			case "serviceTask":
				if err := validServiceTask(j); err != nil {
					return err
				}
			}
		}
		if err := checkVariables(i); err != nil {
			return err
		}
	}
	for _, i := range workflow.Messages {
		if err := validName(i.Name); err != nil {
			return err
		}
	}
	return nil
}

func checkVariables(process *model.Process) error {
	inputVars := make(map[string]struct{})
	outputVars := make(map[string]struct{})
	condVars := make(map[string]struct{})
	for _, e := range process.Elements {
		if e.InputTransform != nil {
			for _, exp := range e.InputTransform {
				v2, err := expression.GetVariables(exp)
				if err != nil {
					return err
				}
				for k := range v2 {
					inputVars[k] = struct{}{}
				}
			}
		}
		if e.OutputTransform != nil {
			for exp := range e.OutputTransform {
				v2, err := expression.GetVariables("=" + exp)
				if err != nil {
					return err
				}
				for k := range v2 {
					outputVars[k] = struct{}{}
				}
			}
		}
		if e.Outbound != nil {
			for _, t := range e.Outbound.Target {
				for _, c := range t.Conditions {
					v2, err := expression.GetVariables(c)
					if err != nil {
						return err
					}
					for k := range v2 {
						condVars[k] = struct{}{}
					}
				}
			}
		}
		if len(e.Errors) > 0 {
			for _, t := range e.Errors {
				for exp := range t.OutputTransform {
					v2, err := expression.GetVariables("=" + exp)
					if err != nil {
						return err
					}
					for k := range v2 {
						outputVars[k] = struct{}{}
					}
				}
			}
		}
	}

	//Test that inputs are all defined
	for i := range inputVars {
		if _, ok := outputVars[i]; !ok {
			return fmt.Errorf("the undefined variable \"%s\" is referred to as input: %w", i, errors2.ErrUndefinedVariable)
		}
	}
	for i := range condVars {
		if _, ok := outputVars[i]; !ok {
			return fmt.Errorf("the undefined variable \"%s\" is referred to in a condition: %w", i, errors2.ErrUndefinedVariable)
		}
	}
	return nil
}

type valError struct {
	Err     error
	Context string
}

func (e valError) Error() string {
	return fmt.Sprintf("%s: %s\n", e.Err.Error(), e.Context)
}

func validServiceTask(j *model.Element) error {
	if j.Execute == "" {
		return &valError{Err: errMissingServiceTaskDefinition, Context: j.Id}
	}
	return nil
}

var validKeyRe = regexp.MustCompile(`\A[-/_=\.a-zA-Z0-9]+\z`)

// is a NATS compatible name
func validName(name string) error {
	if len(name) == 0 || name[0] == '.' || name[len(name)-1] == '.' {
		return fmt.Errorf("'%s' contains invalid characters when used with SHAR", name)
	}
	if !validKeyRe.MatchString(name) {
		return fmt.Errorf("'%s' contains invalid characters when used with SHAR", name)
	}
	return nil
}
