package parser

import (
	"errors"
	"fmt"
	"github.com/antonmedv/expr"
	"gitlab.com/shar-workflow/shar/model"
)

func validExpr(expression string) error {
	_, err := expr.Compile(expression)
	if err != nil {
		return err
	}
	return nil
}

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
