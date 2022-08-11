package parser

import (
	"errors"
	"fmt"
	"github.com/antchfx/xmlquery"
	"gitlab.com/shar-workflow/shar/model"
	"io"
	"strconv"
	"strings"
)

//goland:noinspection HttpUrlsUsage
const bpmnNS = "http://www.omg.org/spec/BPMN/20100524/MODEL"

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
	for _, prXml := range prXmls {
		pr := &model.Process{
			Elements: make([]*model.Element, 0),
		}
		msgXml := doc.SelectElements("//bpmn:message")
		errXml := doc.SelectElements("//bpmn:error")
		pr.Name = prXml.SelectAttr("id")
		if err := parseProcess(doc, wf, prXml, pr, msgXml, errXml, msgs, errs); err != nil {
			return nil, err
		}
		wf.Process[pr.Name] = pr
	}
	return wf, nil
}

func parseProcess(doc *xmlquery.Node, wf *model.Workflow, prXml *xmlquery.Node, pr *model.Process, msgXml []*xmlquery.Node, errXml []*xmlquery.Node, msgs map[string]string, errs map[string]string) error {
	for _, i := range prXml.SelectElements("*") {
		if err := parseElements(doc, wf, pr, i, msgs, errs); err != nil {
			return err
		}
	}
	if msgXml != nil {
		parseMessages(doc, wf, msgXml, msgs)
	}
	if errXml != nil {
		parseErrors(doc, wf, errXml, errs)
	}
	for _, i := range prXml.SelectElements("//bpmn:boundaryEvent") {
		parseBoundaryEvent(i, pr)
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
		parseExtensions(doc, m[i], x)
	}
	wf.Messages = m
}

func parseErrors(doc *xmlquery.Node, wf *model.Workflow, errNodes []*xmlquery.Node, errs map[string]string) {
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

		parseCoreValues(i, el)
		parseFlowInOut(doc, i, el)
		parseDocumentation(i, el)
		parseElementErrors(doc, i, el)
		parseExtensions(doc, el, i)
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

func parseElementErrors(doc *xmlquery.Node, i *xmlquery.Node, el *model.Element) {
	if errorRef := i.SelectElement("//bpmn:errorEventDefinition/@errorRef"); errorRef != nil {
		fmt.Println("error", errorRef.InnerText())
		tg := doc.SelectElement("//*[@id=\"" + errorRef.InnerText() + "\"]")
		fmt.Println(tg.InnerText())
		el.Error = &model.Error{
			Id:   tg.SelectAttr("id"),
			Code: tg.SelectAttr("errorCode"),
			Name: tg.SelectAttr("name"),
		}
	}
}

func parseBoundaryEvent(i *xmlquery.Node, pr *model.Process) {
	attach := i.SelectAttr("attachedToRef")
	var el *model.Element
	for _, i := range pr.Elements {
		if i.Id == attach {
			el = i
			break
		}
	}
	if errorRef := i.SelectElement("//bpmn:errorEventDefinition/@errorRef"); errorRef != nil {
		fmt.Println(attach, errorRef.InnerText())
		allFlow := i.SelectElements("..//bpmn:sequenceFlow")
		flowId := i.SelectElement("//bpmn:outgoing").InnerText()
		var target string
		for _, v := range allFlow {
			if v.SelectAttr("id") == flowId {
				target = v.SelectAttr("targetRef")
			}
		}
		el.Errors = append(el.Errors, &model.CatchError{
			Id:      i.SelectElement("//bpmn:errorEventDefinition/@id").InnerText(),
			ErrorId: errorRef.InnerText(),
			Target:  target,
		})
	}
}

func parseIntermediateCatchEvent(i *xmlquery.Node, _ *model.Element) {
	subType := i.FirstChild
	switch subType.Data {
	case "timerEventDefinition":
	case "messageEventDefinition":

	}
}

func parseSubscription(wf *model.Workflow, el *model.Element, i *xmlquery.Node, msgs map[string]string, errs map[string]string) error {
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
		return "", fmt.Errorf("empty duration found")
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

func parseExtensions(doc *xmlquery.Node, el *model.Element, i *xmlquery.Node) {
	if x := i.SelectElement("bpmn:extensionElements"); x != nil {
		//Task Definitions
		if e := x.SelectElement("zeebe:taskDefinition/@type"); e != nil {
			el.Execute = e.InnerText()
		}
		if e := x.SelectElement("zeebe:taskDefinition/@retries"); e != nil {
			el.Retries = e.InnerText()
		}
		if e := x.SelectElement("zeebe:calledElement/@processId"); e != nil {
			el.Execute = e.InnerText()
		}
		if e := x.SelectElement("zeebe:calledElement/@processId"); e != nil {
			el.Execute = e.InnerText()
		}
		//Form Definitions
		if fk := x.SelectElement("zeebe:formDefinition/@formKey"); fk != nil {
			fullName := strings.Split(fk.InnerText(), ":")
			name := fullName[len(fullName)-1]
			f := doc.SelectElement("//zeebe:userTaskForm[@id=\"" + name + "\"]").InnerText()
			el.Execute = f
		}

		//Assignment Definition
		if e := x.SelectElement("zeebe:assignmentDefinition/@assignee"); e != nil {
			el.Candidates = e.InnerText()
		}

		//Assignment Definition
		if e := x.SelectElement("zeebe:assignmentDefinition/@candidateGroups"); e != nil {
			el.CandidateGroups = e.InnerText()
		}

		//Messages
		if e := x.SelectElement("zeebe:subscription/@correlationKey"); e != nil {
			el.Execute = e.InnerText()
		}
	}
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
	} else {
		return nil
	}
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
