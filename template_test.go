package template

import (
  "testing"
  "github.com/stretchr/testify/assert"
  "bytes"
  "github.com/Lunkov/lib-tr"

  "flag"
  "github.com/golang/glog"
)

func TestCheckTemplate(t *testing.T) {
  flag.Set("alsologtostderr", "true")
  flag.Set("log_dir", ".")
  flag.Set("v", "9")
  flag.Parse()

  glog.Info("Logging configured")

  translate := tr.New()
  templates := NewHTTPTemplates("templates/", translate, false, false)
  
  templates.LoadTemplates("templates/")
  // Public templates
  tmplProp, ok := templates.GetTemplate("index111", "ru_RU")
  assert.Nil(t, tmplProp)
  assert.False(t, ok)

  tmplProp, ok = templates.GetTemplate("index", "ru_RU")
  assert.NotNil(t, tmplProp)
  assert.True(t, ok)

  vars_need := []string{ "Title" } //, "User_DisplayName"}
  vars, _ := templates.requiredTemplateVars(tmplProp)

  assert.Equal(t, vars_need, vars)
  
  trs_need := []string{ "Services", "Exit"}
  trs := templates.findTrTemplate(tmplProp)
  assert.Equal(t, trs_need, trs)

  propPage := map[string]interface{} {
      "Title": "User Info",
      "User_DisplayName": "Serg",
    }
  var ptpl bytes.Buffer
  err := tmplProp.Execute(&ptpl, propPage)
  assert.Nil(t, err)

  assert.Equal(t, "<html>\n\t<head>\n\t\t<meta charset=\"utf-8\">\n\t</head>\n<body>\n\t<div>User Info</div>\n\n<div>Serg</div>\n\n\t<div>ru_RU</div>\n\t<div>Services</div>\n\t<div>Exit</div>\n</body>\n</html>\n", ptpl.String())
}

func TestCheckTemplateMini(t *testing.T) {
  flag.Set("alsologtostderr", "true")
  flag.Set("log_dir", ".")
  flag.Set("v", "9")
  flag.Parse()

  glog.Info("Logging configured")
  
  translate := tr.New()
  templates := NewHTTPTemplates("templates/", translate, false, true)
  templates.LoadTemplates("templates/")  

  // Template Exists 
  assert.False(t, templates.TemplateExists("index111"))
  assert.True(t, templates.TemplateExists("index"))

  // Public templates
  tmplProp, ok := templates.GetTemplate("index111", "ru_RU")
  assert.Nil(t, tmplProp)
  assert.False(t, ok)

  tmplProp, ok = templates.GetTemplate("index", "ru_RU")
  assert.NotNil(t, tmplProp)
  assert.True(t, ok)

  vars_need := []string{ "Title"} //, "User_DisplayName"}
  vars, _ := templates.requiredTemplateVars(tmplProp)

  assert.Equal(t, vars_need, vars)
  
  trs_need := []string{ "Services", "Exit"}
  trs := templates.findTrTemplate(tmplProp)
  assert.Equal(t, trs_need, trs)

  propPage := map[string]interface{} {
      "Title": "User Info",
      "User_DisplayName": "Serg",
    }
  var ptpl bytes.Buffer
  err := tmplProp.Execute(&ptpl, propPage)
  assert.Nil(t, err)

  assert.Equal(t, "<html><head><meta charset=utf-8></head><body><div>User Info</div><div>Serg</div><div>ru_RU</div><div>Services</div><div>Exit</div></body></html>", ptpl.String())
}
