package valueparsing

import (
	"regexp"
	"strconv"

	"gitlab.com/shar-workflow/shar/model"
)

func Parse(arr []string) (*model.Vars, error) {
	vars := model.Vars{}
	for _, elem := range arr {
		key, _type, value := extract(elem)
		switch _type {
		case "int":
			intVal, err := strconv.Atoi(value)
			if err != nil {
				return nil, err
			}
			vars[key] = intVal
		case "float32":
			float32Value, err := strconv.ParseFloat(value, 32)
			if err != nil {
				return nil, err
			}
			vars[key] = float32(float32Value)
		case "float64":
			float64Value, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, err
			}
			vars[key] = float64Value
		case "string":
			vars[key] = value
		}
	}
	return &vars, nil
}

func extract(text string) (key string, _type string, value string) {
	re := regexp.MustCompile(`([A-Za-z0-9]*):([A-Za-z0-9]*)\((.*)\)`)
	arr := re.FindAllStringSubmatch(text, -1)

	key = arr[0][1]
	_type = arr[0][2]
	value = arr[0][3]

	return
}
