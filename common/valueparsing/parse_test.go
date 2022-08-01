package valueparsing

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestExtract(t *testing.T) {
	test_text := `"orderId":int(78)`
	key, _type, value := extract(test_text)
	assert.Equal(t, "orderId", key, "Parser returned wrong value of the key")
	assert.Equal(t, "int", _type, "Parser returned wrong value of the type")
	assert.Equal(t, "78", value, "Parser returned wrong value")
}

func TestParse(t *testing.T) {
	test_args := []string{`"orderId":int(78)`, `"height":float64(103.101)`}
	vars, err := Parse(test_args)
	if err != nil {
		t.Fail()
	}

	expectedOrderId := int(78)
	expectedHeight := float64(103.101)
	assert.Equal(t, expectedOrderId, (*vars)["orderId"], "Parser returned wrong value")
	assert.Equal(t, expectedHeight, (*vars)["height"], "Parser returned wrong value")
}