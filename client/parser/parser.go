package parser

import (
	"errors"
	"fmt"
	"github.com/antchfx/xmlquery"
	"gitlab.com/shar-workflow/shar/model"
	"io"
	"strconv"
	"strings"

	"github.com/relvacode/iso8601"
)

//goland:noinspection HttpUrlsUsage
const bpmnNS = "http://www.omg.org/spec/BPMN/20100524/MODEL"

// Parse parses BPMN, and turns it into a SHAR state machine
func Parse(name string, rdr io.Reader) (*model.Workflow, error) {
	msgs := make(map[string]string)
	errs := make(map[string]string)
	doc, err := xmlquery.Parse(rdr)
	if err != nil {
		return nil, err
	}
	prXmls := doc.SelectElements("//bpmn:process")
	wf := &model.Workflow{
		Name:    name,
		Process: make(map[string]*model.Process, len(prXmls)),
	}
	for _, prXML := range prXmls {
		pr := &model.Process{
			Elements: make([]*model.Element, 0),
		}
		msgXML := doc.SelectElements("//bpmn:message")
		errXML := doc.SelectElements("//bpmn:error")
		pr.Name = prXML.SelectAttr("id")
		if err := parseProcess(doc, wf, prXML, pr, msgXML, errXML, msgs, errs); err != nil {
			return nil, err
		}
		wf.Process[pr.Name] = pr
	}

	if err := validModel(wf); err != nil {
		return nil, err
	}

	return wf, nil
}

func parseProcess(doc *xmlquery.Node, wf *model.Workflow, prXML *xmlquery.Node, pr *model.Process, msgXML []*xmlquery.Node, errXML []*xmlquery.Node, msgs map[string]string, errs map[string]string) error {
	for _, i := range prXML.SelectElements("*") {
		if err := parseElements(doc, wf, pr, i, msgs, errs); err != nil {
			return err
		}
	}
	if msgXML != nil {
		parseMessages(doc, wf, msgXML, msgs)
	}
	if errXML != nil {
		parseErrors(doc, wf, errXML, errs)
	}
	for _, i := range prXML.SelectElements("//bpmn:boundaryEvent") {
		parseBoundaryEvent(doc, i, pr)
	}

	return nil
}

func parseMessages(doc *xmlquery.Node, wf *model.Workflow, msgNodes []*xmlquery.Node, msgs map[string]string) {
	m := make([]*model.Element, len(msgNodes))
	for i, x := range msgNodes {
		m[i] = &model.Element{
			Id:   x.SelectElement("@id").InnerText(),
			Name: x.SelectElement("@name").InnerText(),
		}
		msgs[m[i].Id] = m[i].Name
		parseZeebeExtensions(doc, m[i], x)
	}
	wf.Messages = m
}

func parseErrors(_ *xmlquery.Node, wf *model.Workflow, errNodes []*xmlquery.Node, errs map[string]string) {
	m := make([]*model.Error, len(errNodes))
	for i, x := range errNodes {
		m[i] = &model.Error{
			Id:   x.SelectElement("@id").InnerText(),
			Name: x.SelectElement("@name").InnerText(),
			Code: x.SelectElement("@errorCode").InnerText(),
		}
		errs[m[i].Id] = m[i].Name
	}
	wf.Errors = m
}

func parseElements(doc *xmlquery.Node, wf *model.Workflow, pr *model.Process, i *xmlquery.Node, msgs map[string]string, errs map[string]string) error {
	if i.NamespaceURI == bpmnNS {
		el := &model.Element{Type: i.Data}

		// These are handled specially
		if i.Data == "sequenceFlow" || i.Data == "incoming" || i.Data == "outgoing" || i.Data == "extensionElements" || i.Data == "boundaryEvent" {
			return nil
		}

		// Intermediate catch events need special processing
		if i.Data == "intermediateCatchEvent" {
			parseIntermediateCatchEvent(i, el)
		}

		if i.Data == "startEvent" {
			if err := parseStartEvent(i, el); err != nil {
				return err
			}
		}

		parseCoreValues(i, el)
		parseFlowInOut(doc, i, el)
		parseDocumentation(i, el)
		parseElementErrors(doc, i, el)
		parseZeebeExtensions(doc, el, i)
		if err := parseSubprocess(doc, wf, el, i, msgs, errs); err != nil {
			return err
		}
		if err := parseSubscription(wf, el, i, msgs, errs); err != nil {
			return err
		}
		pr.Elements = append(pr.Elements, el)
	}
	return nil
}

func parseStartEvent(n *xmlquery.Node, el *model.Element) error {
	if def := n.SelectElement("bpmn:timerEventDefinition"); def != nil {
		timeCycle := def.SelectElement("bpmn:timeCycle/text()")
		timeDate := def.SelectElement("bpmn:timeDate/text()")
		if timeCycle == nil && timeDate == nil {
			return errors.New("found timerEventDefinition, but it had no time or Duration specified")
		}
		var t *model.WorkflowTimerDefinition
		if timeDate != nil {
			tval, err := iso8601.ParseString(timeDate.Data)
			if err != nil {
				return err
			}
			t = &model.WorkflowTimerDefinition{
				Type:  model.WorkflowTimerType_fixed,
				Value: tval.UnixNano(),
			}
		} else {
			badFormat := errors.New("time cycle was not in the correct format")
			parts := strings.Split(timeCycle.Data, "/")
			if len(timeCycle.Data) == 0 || len(parts) > 2 || timeCycle.Data[0] != 'R' || !strings.Contains(timeCycle.Data, "/") {
				return badFormat
			}

			repeat, err := strconv.Atoi(parts[0][1:])
			if err != nil {
				return badFormat
			}
			dur, err := ParseISO8601(parts[1])
			if err != nil {
				return badFormat
			}
			t = &model.WorkflowTimerDefinition{
				Type:       model.WorkflowTimerType_duration,
				Value:      dur.timeDuration().Nanoseconds(),
				Repeat:     int64(repeat),
				DropEvents: false,
			}
		}
		el.Timer = t
		el.Type = "timedStartEvent"
	}
	return nil
}

func parseElementErrors(doc *xmlquery.Node, i *xmlquery.Node, el *model.Element) {
	if errorRef := i.SelectElement("//bpmn:errorEventDefinition/@errorRef"); errorRef != nil {
		tg := doc.SelectElement("//*[@id=\"" + errorRef.InnerText() + "\"]")
		el.Error = &model.Error{
			Id:   tg.SelectAttr("id"),
			Code: tg.SelectAttr("errorCode"),
			Name: tg.SelectAttr("name"),
		}
	}
}

func parseBoundaryEvent(doc *xmlquery.Node, i *xmlquery.Node, pr *model.Process) {
	attach := i.SelectAttr("attachedToRef")
	var el *model.Element
	for _, i := range pr.Elements {
		if i.Id == attach {
			el = i
			break
		}
	}
	if errorRef := i.SelectElement("//bpmn:errorEventDefinition/@errorRef"); errorRef != nil {
		allFlow := i.SelectElements("..//bpmn:sequenceFlow")
		flowID := i.SelectElement("//bpmn:outgoing").InnerText()
		var target string
		for _, v := range allFlow {
			if v.SelectAttr("id") == flowID {
				target = v.SelectAttr("targetRef")
			}
		}
		newCatchErr := &model.CatchError{
			Id:      i.SelectElement("//bpmn:errorEventDefinition/@id").InnerText(),
			ErrorId: errorRef.InnerText(),
			Target:  target,
		}
		parseZeebeExtensions(doc, newCatchErr, i)
		el.Errors = append(el.Errors, newCatchErr)
	}
	if timerEvent := i.SelectElement("//bpmn:timerEventDefinition"); timerEvent != nil {
		fmt.Println(attach, timerEvent.InnerText())
		allFlow := i.SelectElements("..//bpmn:sequenceFlow")
		flowID := i.SelectElement("//bpmn:outgoing").InnerText()
		var target string
		for _, v := range allFlow {
			if v.SelectAttr("id") == flowID {
				target = v.SelectAttr("targetRef")
			}
		}
		durationExpr := i.SelectElement("//bpmn:timeDuration").InnerText()
		newTimer := &model.Timer{
			Id:       i.SelectElement("//bpmn:timerEventDefinition/@id").InnerText(),
			Duration: durationExpr,
			Target:   target,
		}
		parseZeebeExtensions(doc, newTimer, i)
		el.BoundaryTimer = append(el.BoundaryTimer, newTimer)
	}
}

func parseIntermediateCatchEvent(i *xmlquery.Node, _ *model.Element) {
	subType := i.FirstChild
	switch subType.Data {
	case "timerEventDefinition":
	case "messageEventDefinition":

	}
}

func parseSubscription(wf *model.Workflow, el *model.Element, i *xmlquery.Node, msgs map[string]string, _ map[string]string) error {
	if i.Data == "intermediateCatchEvent" {
		if x := i.SelectElement("bpmn:messageEventDefinition/@messageRef"); x != nil {
			ref := x.InnerText()
			el.Type = "messageIntermediateCatchEvent"
			el.Msg = msgs[ref]
			c, err := getCorrelation(wf.Messages, el.Msg)
			if err != nil {
				return err
			}
			el.Execute = c
			return nil
		}
		if x := i.SelectElement("bpmn:timerEventDefinition/bpmn:timeDuration"); x != nil {
			ref := x.InnerText()
			el.Type = "timerIntermediateCatchEvent"
			dur, err := parseDuration(ref)
			if err != nil {
				return err
			}
			el.Execute = dur
			return nil
		}
	}
	return nil
}

func parseDuration(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if len(ref) == 0 {
		return "", fmt.Errorf("empty Duration found")
	}
	d, err := ParseISO8601(ref)
	if err != nil {
		if ref[1] == '=' {
			return ref, nil
		}
		return "", err
	}
	return strconv.Itoa(int(d.timeDuration().Nanoseconds())), nil
}

func getCorrelation(messages []*model.Element, msg string) (string, error) {
	for _, v := range messages {
		if v.Name == msg {
			return v.Execute, nil
		}
	}
	return "", errors.New("could not find message: " + msg)
}

func parseSubprocess(doc *xmlquery.Node, wf *model.Workflow, el *model.Element, i *xmlquery.Node, msgs map[string]string, errs map[string]string) error {
	if i.Data == "subProcess" {
		pr := &model.Process{
			Elements: make([]*model.Element, 0),
		}
		if err := parseProcess(doc, wf, i, pr, nil, nil, msgs, errs); err != nil {
			return err
		}
		el.Process = pr
	}
	return nil
}

func parseDocumentation(i *xmlquery.Node, el *model.Element) {
	if e := i.SelectElement("bpmn:documentation"); e != nil {
		el.Documentation = e.InnerText()
	}
}

func parseConditions(i *xmlquery.Node) []string {
	cd := i.SelectElements("bpmn:conditionExpression")
	cds := make([]string, 0, len(cd))

	// Condition
	for _, j := range cd {
		cds = append(cds, j.InnerText())
	}
	if len(cds) > 0 {
		return cds
	}
	return nil
}

func parseCoreValues(i *xmlquery.Node, el *model.Element) {
	if e := i.SelectElement("@id"); e != nil {
		el.Id = e.InnerText()
	}
	if e := i.SelectElement("@name"); e != nil {
		el.Name = e.InnerText()
	}
}

func parseFlowInOut(doc *xmlquery.Node, i *xmlquery.Node, el *model.Element) {
	c2 := i.SelectElements("bpmn:outgoing")
	elo := make([]*model.Target, 0, len(c2))
	for _, c := range c2 {
		sf := doc.SelectElement("//bpmn:sequenceFlow[@id=\"" + c.InnerText() + "\"]")
		tg := doc.SelectElement("//*[@id=\"" + sf.SelectElement("@targetRef").InnerText() + "\"]")
		elo = append(elo, &model.Target{
			Id:         sf.SelectElement("@id").InnerText(),
			Target:     tg.SelectElement("@id").InnerText(),
			Conditions: parseConditions(sf),
		})
	}
	if len(elo) > 0 {
		el.Outbound = &model.Targets{
			Target:    elo,
			Exclusive: i.Data == "exclusiveGateway",
		}
	}
}
