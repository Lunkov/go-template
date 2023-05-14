package template

import (
  "strings"
  "fmt"
  "os"
  "errors"
  "io/ioutil"
  "path/filepath"
  "crypto/md5"
  "encoding/hex"
  "time"
  "regexp"
  "sync"
  
  "github.com/golang/glog"

  "html/template"
  "text/template/parse"
  
  "github.com/Lunkov/lib-tr"
  
  "github.com/radovskyb/watcher"
  "github.com/tdewolff/minify/v2"
  "github.com/tdewolff/minify/v2/css"
  "github.com/tdewolff/minify/v2/html"
  "github.com/tdewolff/minify/v2/js"
  "github.com/tdewolff/minify/v2/svg"
)

type HTTPTemplate struct {
  TemplPath          string

  templates          map[string]*template.Template
  muTemplates        sync.RWMutex

  templatesAddons    map[string]*template.Template
  muTemplatesAddons  sync.RWMutex

  Translate         *tr.Tr
  minifyRender      *minify.M
  watcherFiles      *watcher.Watcher
}

func NewHTTPTemplates(templPath string, translate *tr.Tr, enableWatcher bool, enableMinify bool) *HTTPTemplate {
  p := &HTTPTemplate{TemplPath: templPath, Translate: translate}
  p.Clear()
  p.minifyRender = nil
  if enableMinify {
    p.minifyRender = minify.New()
    p.minifyRender.AddFunc("text/css", css.Minify)
    p.minifyRender.Add("text/html", &html.Minifier{ KeepDocumentTags: true })
    p.minifyRender.AddFunc("image/svg+xml", svg.Minify)
    p.minifyRender.AddFuncRegexp(regexp.MustCompile("^(application|text)/(x-)?(java|ecma)script$"), js.Minify)
    glog.Infof("LOG: Enable Minify HTML")
  }
  if enableWatcher {
    p.watcherFiles = watcher.New()
    p.watcherFiles.SetMaxEvents(1)
    p.watcherFiles.FilterOps(watcher.Rename, watcher.Move, watcher.Remove, watcher.Create, watcher.Write)
    go func() {
      for {
        select {
        case event := <-p.watcherFiles.Event:	
          if glog.V(9) {
            glog.Infof("DBG: Watcher Event: %v", event)
          }
          // Ignore New Translate Files
          if filepath.Ext(event.Name()) != ".!yaml" {
            if glog.V(9) {
              glog.Infof("DBG: Watcher File: %v", event.Name())
            }
            p.Clear()
          }
        case err := <-p.watcherFiles.Error:
          glog.Fatalf("ERR: Watcher Event: %v", err)
        case <-p.watcherFiles.Closed:
          glog.Infof("LOG: Watcher Close")
          return
        }
      }
    }()
    // Start the watching process - it'll check for changes every 100ms.
    glog.Infof("LOG: Watcher Start (%s)", templPath)
    if err := p.watcherFiles.AddRecursive(templPath); err != nil {
      glog.Fatalf("ERR: Watcher AddRecursive: %v", err)
    }
    
    if glog.V(9) {
      // Print a list of all of the files and folders currently
      // being watched and their paths.
      for path, f := range p.watcherFiles.WatchedFiles() {
        glog.Infof("DBG: WATCH FILE: %s: %s\n", path, f.Name())
      }
    }
	  go func() {
      if err := p.watcherFiles.Start(time.Millisecond * 100); err != nil {
        glog.Fatalf("ERR: Watcher Start: %v", err)
      }
    }()
  }

  return p
}

func (p *HTTPTemplate) Clear() {
  p.muTemplates.Lock()
  p.templates = make(map[string]*template.Template)
  p.muTemplates.Unlock()

  p.muTemplatesAddons.Lock()
  p.templatesAddons = make(map[string]*template.Template)
  p.muTemplatesAddons.Unlock()
}

func (p *HTTPTemplate) FuncMap(lang string) template.FuncMap {
  return template.FuncMap{
          "TR": func(str string) string {
              t, _ := p.Translate.Tr(lang, str)
              return t
          },
          "TR_LANG": func() string {
              return lang
          },
          "TR_LANG_NAME": func() string {
              return p.Translate.LangName(lang)
          },
          "TR_LANGS_LIST": func() *map[string]map[string]string {
              return p.Translate.GetList()
          },
          "hash":func(s string) string {
              hasher := md5.New()
              hasher.Write([]byte(s))
              return hex.EncodeToString(hasher.Sum(nil))
          },
          "js":func(s string) template.JS {
              return template.JS(s)
          },
          "attr":func(s string) template.HTMLAttr {
              return template.HTMLAttr(s)
          },
          "safe": func(s string) template.HTML {
              return template.HTML(s)
          },
          "url": func(s string) template.URL {
              return template.URL(s)
          },
          "args": argsfn,
        }
}

func argsfn(kvs ...interface{}) (map[string]interface{}, error) {
  if len(kvs)%2 != 0 {
    return nil, errors.New("args requires even number of arguments.")
  }
  m := make(map[string]interface{})
  for i := 0; i < len(kvs); i += 2 {
    s, ok := kvs[i].(string)
    if !ok {
        return nil, errors.New("even args to args must be strings.")
    }
    m[s] = kvs[i+1]
  }
  return m, nil
}

func (p *HTTPTemplate) GetTemplate(path string, lang string) (*template.Template, bool) {
  filen := fmt.Sprintf("%s/%s.html", p.TemplPath, path)
  filename, err := filepath.Abs(filen)
  if err != nil {
    glog.Errorf("ERR: Get AbsPath(%s): %v", filen, err)
    return nil, false
  }
  index := p.makeIndex(filename, lang)
  p.muTemplates.RLock()
  i, ok := p.templates[index]
  p.muTemplates.RUnlock()
  if ok {
    return i, true
  }
  t, okt := p.loadTemplateFromFile(filename, lang)
  if !okt {
    return nil, false
  } 
  t = p.appendBaseTemplate(t, p.TemplPath + "/base", lang)
  t = p.appendBaseTemplate(t, p.TemplPath + "/components", lang)
  p.muTemplates.Lock()
  p.templates[index] = t
  p.muTemplates.Unlock()
  return t, true
}

func (p *HTTPTemplate) MakeTrMap(t *template.Template, lang string) map[string]string {
  resTr := make(map[string]string)
  trs := p.findTrTemplate(t)
  for _, v := range trs {
    p.Translate.SetDef(v)
    resTr[v], _ = p.Translate.Tr(lang, v)
  }
  return resTr
}

// Get Name of Template from file name
func (p *HTTPTemplate) makeIndex(fileName string, lang string) string {
  hasher := md5.New()
  hasher.Write([]byte(fileName))
  hasher.Write([]byte("*"))
  hasher.Write([]byte(lang))
  return hex.EncodeToString(hasher.Sum(nil))
}

func (p *HTTPTemplate) appendBaseTemplate(t *template.Template, path string, lang string) *template.Template {
  scanPath, err := filepath.Abs(path)
  if err != nil {
    glog.Errorf("ERR: Get AbsPath(%s): %v", scanPath, err)
    return nil
  }
  if glog.V(9) {
    glog.Infof("DBG: Scan Templates(%s)", scanPath)
  }
  count := 0
  errScan := filepath.Walk(scanPath, func(filename string, f os.FileInfo, err error) error {
    if f != nil && f.IsDir() == false {
      if glog.V(2) {
        glog.Infof("LOG: Loading template(%s) file: %s\n", path, filename)
      }

      index := p.makeIndex(filename, lang)
      p.muTemplatesAddons.RLock()
      t_base, ok := p.templatesAddons[index]
      p.muTemplatesAddons.RUnlock()
      if !ok {
        t_base, ok = p.loadTemplateFromFile(filename, lang)
        if !ok {
          return nil
        }
        p.MakeTrMap(t_base, lang)
        if glog.V(9) {
          glog.Infof("DBG: Append Template Addon: %s", t_base.Name())
        }
        p.muTemplatesAddons.Lock()
        p.templatesAddons[index] = t_base
        p.muTemplatesAddons.Unlock()
      }
      count ++
      t.AddParseTree(t_base.Name(), t_base.Tree)
    }
    return nil
  })
  if glog.V(9) {
    glog.Infof("DBG: Scan Path: %s, Templates: %d", scanPath, count)
  }
  if errScan != nil {
    glog.Errorf("ERR: %s\n", errScan)
  }
  return t
}

func (p *HTTPTemplate) LoadTemplates(path string, lang string) {
  scanPath, err := filepath.Abs(path)
  if err != nil {
    glog.Errorf("ERR: Get AbsPath(%s): %v", scanPath, err)
    return
  }
  if glog.V(9) {
    glog.Infof("DBG: Scan Templates(%s)", scanPath)
  }
  count := 0
  errScan := filepath.Walk(scanPath, func(filename string, f os.FileInfo, err error) error {
    if f != nil && f.IsDir() == false {
      if glog.V(2) {
        glog.Infof("LOG: Loading template: %s", filename)
      }
      index := p.makeIndex(filename, lang)
      p.muTemplates.RLock()
      t_base, ok := p.templates[index]
      p.muTemplates.RUnlock()
      if !ok {
        t_base, ok = p.loadTemplateFromFile(filename, lang)
        if !ok {
          glog.Errorf("ERR: Get Template(%s)", filename)
          return nil
        }
        p.MakeTrMap(t_base, lang)
        p.muTemplates.Lock()
        p.templates[index] = t_base
        p.muTemplates.Unlock()
      }
      count ++
    }
    return nil
  })
  if glog.V(9) {
    glog.Infof("DBG: Scan Path: %s, Templates: %d", scanPath, count)
  }
  if errScan != nil {
    glog.Errorf("ERR: %s\n", errScan)
  }
}

// Extract the template vars required from *simple* templates.
// Only works for top level, plain variables. Returns all problematic parse.Node as errors.
func (p *HTTPTemplate) requiredTemplateVars(t *template.Template) ([]string, []error) {
  var res []string
  var errors []error
  var ln *parse.ListNode
  if t == nil {
    return res, errors
  }
  ln = t.Tree.Root
Node:
  for _, n := range ln.Nodes {
    if nn, ok := n.(*parse.ActionNode); ok {
      p := nn.Pipe
      if len(p.Decl) > 0 {
        errors = append(errors, fmt.Errorf("len(p.Decl): Node %v not supported", n))
        continue Node
      }
      for _, c := range p.Cmds {
        if len(c.Args) != 1 {
          errors = append(errors, fmt.Errorf("len(c.Args)=%d: Node %v not supported", len(c.Args), n))
          continue Node
        }
        if a, ok := c.Args[0].(*parse.FieldNode); ok {
          if len(a.Ident) != 1 {
              errors = append(errors, fmt.Errorf("len(a.Ident): Node %v not supported", n))
              continue Node
          }
          res = append(res, a.Ident[0])
        } else {
          errors = append(errors, fmt.Errorf("parse.FieldNode: Node %v not supported", n))
          continue Node
        }

      }
    } else {
      if _, ok := n.(*parse.TextNode); !ok {
        errors = append(errors, fmt.Errorf("parse.TextNode: Node %v not supported", n))
        continue Node
      }
    }
  }
  return res, errors
}

// Extract the template vars required from *simple* templates.
// Only works for top level, plain variables. Returns all problematic parse.Node as errors.
func (p *HTTPTemplate) findTrTemplate(t *template.Template) []string {
  var res []string
  if t == nil || t.Tree == nil  || t.Tree.Root == nil {
    return res
  }
  var ln *parse.ListNode
  ln = t.Tree.Root
Node:
  for _, n := range ln.Nodes {
    if nn, ok := n.(*parse.ActionNode); ok {
      p := nn.Pipe
      for _, c := range p.Cmds {
        if len(c.Args) == 2 {
          if c.Args[0].String() == "TR" {
            str := strings.ReplaceAll(c.Args[1].String(), "\"", "")
            str = strings.ReplaceAll(str, "'", "")
            res = append(res, str)
          } else {
            continue Node
          }
        }
      }
    }
  }
  return res
}

func (p *HTTPTemplate) makeMimiHTML(s string) string {
  if p.minifyRender != nil {
    res, err := p.minifyRender.String("text/html", s)
    if err != nil {
      glog.Errorf("ERR: HTML Minify: %v", err)
    } else {
      return res
    }
  }
  return s
}

// Get Name of Template from file name
func (p *HTTPTemplate) fileNameWithoutExtension(fileName string) string {
  return strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
}

func (p *HTTPTemplate) loadTemplateFromFile(filename string, lang string) (*template.Template, bool) {
  contents, err := ioutil.ReadFile(filename)
  if err != nil {
    glog.Errorf("ERR: Get Template(%s): %v", filename, err)
    return nil, false
  }
  t_base := template.New(p.fileNameWithoutExtension(filename)).Funcs(p.FuncMap(lang))
  t_base, err = t_base.Parse(p.makeMimiHTML(string(contents)))
  if err != nil {
    glog.Errorf("ERR: Parse Template(%s): %v", filename, err)
    if glog.V(9) {
      glog.Infof("DBG: ERROR: Parse Template(%s) html=%s", filename, string(contents))
    }
    return nil, false
  }
  return t_base, true
}
