package parser

import (
	errors2 "errors"
	"fmt"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/element"
	"gitlab.com/shar-workflow/shar/model"
)

func tagWorkflow(wf *model.Workflow) {
	for _, process := range wf.Process {
		els := make(map[string]*model.Element)
		back := getInbound(process)
		common.IndexProcessElements(process.Elements, els)
		tagGateways(process, els, back)
	}
}

func tagGateways(process *model.Process, els map[string]*model.Element, back map[string][]string) error {
	for _, el := range process.Elements {
		if el.Type == element.Gateway {
			var numIn, numOut int
			if ins, ok := back[el.Id]; ok {
				numIn = len(ins)
			}
			if el.Outbound != nil {
				numOut = len(el.Outbound.Target)
			}
			if numIn == 1 && numOut > 1 {
				el.Gateway.Direction = model.GatewayDirection_divergent
			} else if numIn > 1 && numOut == 1 {
				el.Gateway.Direction = model.GatewayDirection_convergent
			} else {
				// Bad Gateway... 503 :-)
				return fmt.Errorf("cannot discern gateway type due to ambiguous inputs and outputs: %w", errors2.New("unsupported gateway type"))
			}
		}
	}
	return nil
}

func getInbound(process *model.Process) map[string][]string {
	backCx := make(map[string][]string)
	for _, el := range process.Elements {
		if el.Outbound != nil {
			for _, c := range el.Outbound.Target {
				if _, ok := backCx[c.Id]; !ok {
					backCx[c.Id] = make([]string, 0)
				}
				backCx[c.Id] = append(backCx[c.Id], el.Id)
			}
		}
	}
	return backCx
}
