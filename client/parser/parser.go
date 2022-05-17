package parser

import (
	"github.com/antchfx/xmlquery"
	"github.com/crystal-construct/shar/model"
	"io"

	"strings"
)

//goland:noinspection HttpUrlsUsage
const bpmnNS = "http://www.omg.org/spec/BPMN/20100524/MODEL"

func Parse(rdr io.Reader) (*model.Process, error) {

	pr := &model.Process{
		Elements: make([]*model.Element, 0),
	}

	doc, err := xmlquery.Parse(rdr)
	if err != nil {
		return nil, err
	}
	prXml := doc.SelectElements("//bpmn:process")[0]
	pr.Name = prXml.SelectAttr("id")
	parseProcess(doc, prXml, pr)
	return pr, nil
}

func parseProcess(doc *xmlquery.Node, prXml *xmlquery.Node, pr *model.Process) {
	for _, i := range prXml.SelectElements("*") {
		parseElements(doc, pr, i)
	}
}

func parseElements(doc *xmlquery.Node, pr *model.Process, i *xmlquery.Node) {
	if i.NamespaceURI == bpmnNS {
		el := &model.Element{Type: i.Data}
		if i.Data == "sequenceFlow" || i.Data == "incoming" || i.Data == "outgoing" || i.Data == "extensionElements" {
			return
		}
		parseCoreValues(i, el)
		parseFlowInOut(doc, i, el)
		parseDocumentation(i, el)
		parseExtensions(doc, el, i)
		parseSubprocess(doc, el, i)
		pr.Elements = append(pr.Elements, el)
	}
}

func parseSubprocess(doc *xmlquery.Node, el *model.Element, i *xmlquery.Node) {
	if i.Data == "subProcess" {
		pr := &model.Process{
			Elements: make([]*model.Element, 0),
		}
		parseProcess(doc, i, pr)
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
