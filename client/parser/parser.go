package parser

import (
	"github.com/antchfx/xmlquery"
	"github.com/crystal-construct/shar/model"
	"io"

	"strings"
)

//goland:noinspection HttpUrlsUsage
const bpmnNS = "http://www.omg.org/spec/BPMN/20100524/MODEL"

func Parse(name string, rdr io.Reader) (*model.Workflow, error) {
	msgs := make(map[string]string)
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
		pr.Name = prXml.SelectAttr("id")
		parseProcess(doc, wf, prXml, pr, msgXml, msgs)
		wf.Process[pr.Name] = pr
	}
	return wf, nil
}

func parseProcess(doc *xmlquery.Node, wf *model.Workflow, prXml *xmlquery.Node, pr *model.Process, msgXml []*xmlquery.Node, msgs map[string]string) {
	for _, i := range prXml.SelectElements("*") {
		parseElements(doc, wf, pr, i, msgs)
	}
	if msgXml != nil {
		parseMessages(doc, wf, pr, msgXml, msgs)
	}
}

func parseMessages(doc *xmlquery.Node, wf *model.Workflow, pr *model.Process, msgNodes []*xmlquery.Node, msgs map[string]string) {
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

func parseElements(doc *xmlquery.Node, wf *model.Workflow, pr *model.Process, i *xmlquery.Node, msgs map[string]string) {
	if i.NamespaceURI == bpmnNS {
		el := &model.Element{Type: i.Data}
		if i.Data == "sequenceFlow" || i.Data == "incoming" || i.Data == "outgoing" || i.Data == "extensionElements" {
			return
		}
		parseCoreValues(i, el)
		parseFlowInOut(doc, i, el)
		parseDocumentation(i, el)
		parseExtensions(doc, el, i)
		parseSubprocess(doc, wf, el, i, msgs)
		parseSubscription(el, i, msgs)
		pr.Elements = append(pr.Elements, el)
	}
}

func parseSubscription(el *model.Element, i *xmlquery.Node, msgs map[string]string) {
	if x := i.SelectElement("bpmn:messageEventDefinition/@messageRef"); x != nil {
		ref := x.InnerText()
		el.Msg = msgs[ref]
	}
}

func parseSubprocess(doc *xmlquery.Node, wf *model.Workflow, el *model.Element, i *xmlquery.Node, msgs map[string]string) {
	if i.Data == "subProcess" {
		pr := &model.Process{
			Elements: make([]*model.Element, 0),
		}
		parseProcess(doc, wf, i, pr, nil, msgs)
		el.Process = pr
	}
}

func parseExtensions(doc *xmlquery.Node, el *model.Element, i *xmlquery.Node) {
	if x := i.SelectElement("bpmn:extensionElements"); x != nil {
		//Task Definitions
		if e := x.SelectElement("zeebe:taskDefinition/@type"); e != nil {
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
