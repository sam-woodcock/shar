package parser

import (
	"errors"
	"fmt"
	"gitlab.com/shar-workflow/shar/common/expression"
	"gitlab.com/shar-workflow/shar/model"
)

var (
	ErrMissingId                    = errors.New("missing id")
	ErrMissingServiceTaskDefinition = errors.New("missing service task definition")
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
				return &ParserError{Err: ErrMissingId, Context: j.Name}
			}
			switch j.Type {
			case "serviceTask":
				if err := validServiceTask(j); err != nil {
					return err
				}
			}
		}
		if err := checkVariables(workflow, i); err != nil {
			return err
		}
	}

	return nil
}

func checkVariables(workflow *model.Workflow, process *model.Process) error {
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
	}

	//Test that inputs are all defined
	for i := range inputVars {
		if _, ok := outputVars[i]; !ok {
			return fmt.Errorf("the undefined variable \"%s\" is referred to as input\n", i)
		}
	}
	for i := range condVars {
		if _, ok := outputVars[i]; !ok {
			return fmt.Errorf("the undefined variable \"%s\" is referred to in a condition\n", i)
		}
	}
	return nil
}

type ParserError struct {
	Err     error
	Context string
}

func (e ParserError) Error() string {
	return fmt.Sprintf("%s: %s\n", e.Err.Error(), e.Context)
}

func validServiceTask(j *model.Element) error {
	if j.Execute == "" {
		return &ParserError{Err: ErrMissingServiceTaskDefinition, Context: j.Id}
	}
	return nil
}

func validName(name string) error {
	return nil
}
