package integration_test

import "github.com/MarcoPoloResearchLab/Lo/internal/catalog"

func jsonFormattingCatalogResults() map[string]string {
	return map[string]string{
		catalog.JSONDecode:       `{"type":"JSON.Value","value":{"kind":"null","value":null}}`,
		catalog.JSONGet:          `{"type":"JSON.Value","value":{"kind":"bool","value":true}}`,
		catalog.JSONAt:           `{"type":"JSON.Value","value":{"kind":"null","value":null}}`,
		catalog.JSONString:       `{"type":"String","value":"text"}`,
		catalog.JSONBool:         `{"type":"Bool","value":true}`,
		catalog.JSONInt:          `{"type":"Int","value":"123"}`,
		catalog.JSONFloat:        `{"type":"Float","value":"1.25"}`,
		catalog.JSONKind:         `{"type":"String","value":"array"}`,
		catalog.JSONLength:       `{"type":"Int","value":"2"}`,
		catalog.IntParse:         `{"type":"Int","value":"123"}`,
		catalog.FloatFormatFixed: `{"type":"String","value":"1.12"}`,
		catalog.StringFormat:     `{"type":"String","value":"  -1,234"}`,
	}
}
