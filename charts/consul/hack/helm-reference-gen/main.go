package main

// This script generates markdown documentation out of the values.yaml file
// for use on consul.io.
//
// Usage: make gen-docs [consul-repo-path] [-validate]
//        Where [consul-repo-path] is the location of the hashicorp/consul repo. Defaults to ../../../consul.
//        If -validate is set, the generated docs won't be output anywhere.
//        This is useful in CI to ensure the generation will succeed.

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

var (
	// typeAnnotation matches the @type annotation. It captures the value of @type.
	typeAnnotation = regexp.MustCompile(`(?m).*@type: (.*)$`)

	// defaultAnnotation matches the @default annotation. It captures the value of @default.
	defaultAnnotation = regexp.MustCompile(`(?m).*@default: (.*)$`)

	// recurseAnnotation matches the @recurse annotation. It captures the value of @recurse.
	recurseAnnotation = regexp.MustCompile(`(?m).*@recurse: (.*)$`)

	// commentPrefix matches on the YAML comment prefix, e.g.
	// ```
	// # comment here
	//   # comment with indent
	// ```
	// Will match on "comment here" and "comment with indent".
	//
	// It also properly handles YAML comments inside code fences, e.g.
	// ```
	// # Example:
	// # ```yaml
	// # # yaml comment
	// # ````
	// ```
	// And will not match the "# yaml comment" incorrectly.
	commentPrefix = regexp.MustCompile(`(?m)^[^\S\n]*#[^\S\n]?`)

	// docNodeTmpl is the go template used to print a DocNode node.
	// We use $ instead of ` in the template so we can use the golang raw string
	// format. We then do the replace from $ => `.
	docNodeTmpl = template.Must(
		template.New("").Parse(
			strings.Replace(
				`{{- if eq .Column 1 }}### {{ .Key }}

{{ end }}{{ .LeadingIndent }}- ${{ .Key }}$ ((#v{{ .HTMLAnchor }})){{ if ne .FormattedKind "" }} (${{ .FormattedKind }}{{ if .FormattedDefault }}: {{ .FormattedDefault }}{{ end }}$){{ end }}{{ if .FormattedDocumentation}} - {{ .FormattedDocumentation }}{{ end }}`,
				"$", "`", -1)),
	)
)

func main() {
	validateFlag := flag.Bool("validate", false, "only validate that the markdown can be generated, don't actually generate anything")
	consulRepoPath := "../../../consul"
	flag.Parse()

	if len(os.Args) > 3 {
		fmt.Println("Error: extra arguments")
		os.Exit(1)
	}

	if !*validateFlag {
		// Only argument is path to Consul repo. If not set then we default.
		if len(os.Args) < 2 {
			abs, _ := filepath.Abs(consulRepoPath)
			fmt.Printf("Defaulting to Consul repo path: %s\n", abs)
		} else {
			// Support absolute and relative paths to the Consul repo.
			if filepath.IsAbs(os.Args[1]) {
				consulRepoPath = os.Args[1]
			} else {
				consulRepoPath = filepath.Join("../..", os.Args[1])
			}
			abs, _ := filepath.Abs(consulRepoPath)
			fmt.Printf("Using Consul repo path: %s\n", abs)
		}
	}

	// Parse the values.yaml file.
	inputBytes, err := ioutil.ReadFile("../../values.yaml")
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	out, err := GenerateDocs(string(inputBytes))
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	// If we're just validating that generation will succeed then we're done.
	if *validateFlag {
		fmt.Println("Validation successful")
		os.Exit(0)
	}

	// Otherwise we'll go on to write the changes to the helm docs.
	helmReferenceFile := filepath.Join(consulRepoPath, "website/content/docs/k8s/helm.mdx")
	helmReferenceBytes, err := ioutil.ReadFile(helmReferenceFile)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	helmReferenceContents := string(helmReferenceBytes)

	// Swap out the contents between the codegen markers.
	startStr := "<!-- codegen: start -->\n\n"
	endStr := "\n  <!-- codegen: end -->"
	start := strings.Index(helmReferenceContents, startStr)
	if start == -1 {
		fmt.Printf("%q not found in %q\n", startStr, helmReferenceFile)
		os.Exit(1)
	}
	end := strings.Index(helmReferenceContents, endStr)
	if end == -1 {
		fmt.Printf("%q not found in %q\n", endStr, helmReferenceFile)
		os.Exit(1)
	}

	newMdx := helmReferenceContents[0:start+len(startStr)] + out + helmReferenceContents[end:]
	err = ioutil.WriteFile(helmReferenceFile, []byte(newMdx), 0644)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	abs, _ := filepath.Abs(helmReferenceFile)
	fmt.Printf("Updated with generated docs: %s\n", abs)
}

func GenerateDocs(yamlStr string) (string, error) {
	node, err := Parse(yamlStr)
	if err != nil {
		return "", err
	}

	children, err := generateDocsFromNode(docNodeTmpl, node)
	return strings.ReplaceAll(strings.Join(children, "\n\n"), "[Enterprise Only]", "<EnterpriseAlert inline />"), err
}

// Parse parses yamlStr into a tree of DocNode's.
func Parse(yamlStr string) (DocNode, error) {
	var node yaml.Node
	err := yaml.Unmarshal([]byte(yamlStr), &node)
	if err != nil {
		return DocNode{}, err
	}

	// Due to how the YAML is parsed this is the first real node.
	rootNode := node.Content[0].Content
	children, err := parseNodeContent(rootNode, "", false)
	if err != nil {
		return DocNode{}, err
	}
	return DocNode{
		Column:   0,
		Children: children,
	}, nil
}

// parseNodeContent recursively parses the yaml nodes and outputs a DocNode
// tree.
func parseNodeContent(nodeContent []*yaml.Node, parentBreadcrumb string, parentWasMap bool) ([]DocNode, error) {
	var docNodes []DocNode

	// This is a special type of node where it's an array of maps.
	// e.g.
	// ````
	// ingressGateways:
	// - name: name
	// ````
	//
	// In this case we show the docs as:
	// - ingress-gateway: ingress gateway descrip
	//   - name: name descrip.
	//
	// To do that, we actually need to skip the map node.
	if len(nodeContent) == 1 {
		return parseNodeContent(nodeContent[0].Content, parentBreadcrumb, true)
	}

	// skipNext is true if we should skip the next node. Due to how the YAML is
	// parsed, a key: value pair results in two YAML nodes but we only need
	// doc node out of that so in the loop we look ahead to the next node
	// and use it to construct our DocNode. Then we can skip it on the next
	// iteration.
	skipNext := false
	for i, child := range nodeContent {
		if skipNext {
			skipNext = false
			continue
		}

		docNode, err := buildDocNode(i, child, nodeContent, parentBreadcrumb, parentWasMap)
		if err != nil {
			return nil, err
		}

		if err := docNode.Validate(); err != nil {
			return nil, &ParseError{
				FullAnchor: docNode.HTMLAnchor(),
				Err:        err.Error(),
			}
		}

		docNodes = append(docNodes, docNode)
		skipNext = true
		continue
	}
	return docNodes, nil
}

func generateDocsFromNode(tm *template.Template, node DocNode) ([]string, error) {
	var out []string
	for _, child := range node.Children {
		var nodeOut bytes.Buffer
		err := tm.Execute(&nodeOut, child)
		if err != nil {
			return nil, err
		}
		childOut, err := generateDocsFromNode(tm, child)
		if err != nil {
			return nil, err
		}
		out = append(append(out, nodeOut.String()), childOut...)
	}
	return out, nil
}

// allScalars returns true if content contains only scalar nodes
// with no chidren.
func allScalars(content []*yaml.Node) bool {
	for _, n := range content {
		if n.Kind != yaml.ScalarNode || len(n.Content) > 0 {
			return false
		}
	}
	return true
}

// toInlineYaml will return the yaml string representation for content
// using the inline representation, i.e. `["a", "b"]`
// instead of:
// ```
// - "a"
// - "b"
// ```
func toInlineYaml(content []*yaml.Node) (string, error) {
	// We have to use this struct so we can set the struct tag "flow" so the
	// generated yaml uses the inline format.
	type intermediary struct {
		Arr []*yaml.Node `yaml:"arr,flow"`
	}
	i := intermediary{
		Arr: content,
	}
	out, err := yaml.Marshal(i)
	if err != nil {
		return "", err
	}
	// Hack: because we had to use our struct, it has the key "arr: " which
	// we need to trim. Before trimming it will look like:
	// `arr: ["a","b"]`.
	return strings.TrimPrefix(string(out), "arr: "), nil
}

func buildDocNode(nodeContentIdx int, currNode *yaml.Node, nodeContent []*yaml.Node, parentBreadcrumb string, parentWasMap bool) (DocNode, error) {
	// Check for the @recurse: false annotation.
	// In this case we construct our node and then don't recurse further.
	if match := recurseAnnotation.FindStringSubmatch(currNode.HeadComment); len(match) > 0 && match[1] == "false" {
		return DocNode{
			Column:           currNode.Column,
			ParentBreadcrumb: parentBreadcrumb,
			ParentWasMap:     false,
			Key:              currNode.Value,
			Comment:          currNode.HeadComment,
		}, nil
	}

	// Nodes should come in pairs.
	if len(nodeContent) < nodeContentIdx+1 {
		return DocNode{}, &ParseError{
			ParentAnchor: parentBreadcrumb,
			CurrAnchor:   currNode.Value,
			Err:          fmt.Sprintf("content length incorrect, expected %d got %d", nodeContentIdx+1, len(nodeContent)),
		}
	}

	next := nodeContent[nodeContentIdx+1]

	switch next.Kind {

	// If it's a scalar then this is a simple key: value node.
	case yaml.ScalarNode:
		return DocNode{
			ParentBreadcrumb: parentBreadcrumb,
			ParentWasMap:     parentWasMap,
			Column:           currNode.Column,
			Key:              currNode.Value,
			Comment:          currNode.HeadComment,
			KindTag:          next.Tag,
			Default:          next.Value,
		}, nil

	// If it's a map then we will need to recurse into it.
	case yaml.MappingNode:
		docNode := DocNode{
			ParentBreadcrumb: parentBreadcrumb,
			ParentWasMap:     parentWasMap,
			Column:           currNode.Column,
			Key:              currNode.Value,
			Comment:          currNode.HeadComment,
			KindTag:          next.Tag,
		}
		var err error
		docNode.Children, err = parseNodeContent(next.Content, docNode.HTMLAnchor(), false)
		if err != nil {
			return DocNode{}, err
		}
		return docNode, nil

	// If it's a sequence, i.e. array, then we have to handle it differently
	// depending on its contents.
	case yaml.SequenceNode:
		// If it's empty then its just a key with a default of empty array.
		if len(next.Content) == 0 {
			return DocNode{
				ParentBreadcrumb: parentBreadcrumb,
				ParentWasMap:     parentWasMap,
				Column:           currNode.Column,
				Key:              currNode.Value,
				// Default is empty array.
				Default: "[]",
				Comment: currNode.HeadComment,
				KindTag: next.Tag,
			}, nil

			// If it's full of scalars, e.g. key: [a, b] then we can stop recursing
			// and use the value as the default.
		} else if allScalars(next.Content) {
			inlineYaml, err := toInlineYaml(next.Content)
			if err != nil {
				return DocNode{}, &ParseError{
					ParentAnchor: parentBreadcrumb,
					CurrAnchor:   currNode.Value,
					Err:          err.Error(),
				}
			}
			return DocNode{
				ParentBreadcrumb: parentBreadcrumb,
				ParentWasMap:     parentWasMap,
				Column:           currNode.Column,
				Key:              currNode.Value,
				// Default will be the yaml value.
				Default: inlineYaml,
				Comment: currNode.HeadComment,
				KindTag: next.Tag,
			}, nil
		} else {

			// Otherwise we need to recurse into each element of the array.
			docNode := DocNode{
				ParentBreadcrumb: parentBreadcrumb,
				ParentWasMap:     parentWasMap,
				Column:           currNode.Column,
				Key:              currNode.Value,
				Comment:          currNode.HeadComment,
				KindTag:          next.Tag,
			}
			var err error
			docNode.Children, err = parseNodeContent(next.Content, docNode.HTMLAnchor(), false)
			if err != nil {
				return DocNode{}, err
			}
			return docNode, nil
		}
	}
	return DocNode{}, fmt.Errorf("fell through cases unexpectedly at breadcrumb: %s", parentBreadcrumb)
}
