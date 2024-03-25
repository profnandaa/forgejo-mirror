// Copyright The Forgejo Authors.
// SPDX-License-Identifier: MIT

package markup

import (
	"bytes"
	"html/template"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"code.gitea.io/gitea/modules/charset"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/translation"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// filePreviewPattern matches "http://domain/org/repo/src/commit/COMMIT/filepath#L1-L2"
var filePreviewPattern = regexp.MustCompile(`https?://((?:\S+/){3})src/commit/([0-9a-f]{4,64})/(\S+)#(L\d+(?:-L\d+)?)`)

type FilePreview struct {
	fileContent []template.HTML
	subTitle    template.HTML
	lineOffset  int
	urlFull     string
	filePath    string
	start       int
	end         int
}

func NewFilePreview(ctx *RenderContext, node *html.Node, locale translation.Locale) *FilePreview {
	preview := &FilePreview{}

	m := filePreviewPattern.FindStringSubmatchIndex(node.Data)
	if m == nil {
		return nil
	}

	// Ensure that every group has a match
	if slices.Contains(m, -1) {
		return nil
	}

	preview.urlFull = node.Data[m[0]:m[1]]

	// Ensure that we only use links to local repositories
	if !strings.HasPrefix(preview.urlFull, setting.AppURL+setting.AppSubURL) {
		return nil
	}

	projPath := strings.TrimSuffix(node.Data[m[2]:m[3]], "/")

	commitSha := node.Data[m[4]:m[5]]
	preview.filePath = node.Data[m[6]:m[7]]
	hash := node.Data[m[8]:m[9]]

	preview.start = m[0]
	preview.end = m[1]

	projPathSegments := strings.Split(projPath, "/")
	fileContent, err := DefaultProcessorHelper.GetRepoFileContent(
		ctx.Ctx,
		projPathSegments[len(projPathSegments)-2],
		projPathSegments[len(projPathSegments)-1],
		commitSha, preview.filePath,
	)
	if err != nil {
		return nil
	}

	lineSpecs := strings.Split(hash, "-")
	lineCount := len(fileContent)

	commitLinkBuffer := new(bytes.Buffer)
	err = html.Render(commitLinkBuffer, createLink(node.Data[m[0]:m[5]], commitSha[0:7], "text black"))
	if err != nil {
		log.Error("failed to render commitLink: %v", err)
	}

	if len(lineSpecs) == 1 {
		line, _ := strconv.Atoi(strings.TrimPrefix(lineSpecs[0], "L"))
		if line < 1 || line > lineCount {
			return nil
		}

		preview.fileContent = fileContent[line-1 : line]
		preview.subTitle = locale.Tr(
			"markup.filepreview.line", line,
			template.HTML(commitLinkBuffer.String()),
		)

		preview.lineOffset = line - 1
	} else {
		startLine, _ := strconv.Atoi(strings.TrimPrefix(lineSpecs[0], "L"))
		endLine, _ := strconv.Atoi(strings.TrimPrefix(lineSpecs[1], "L"))

		if startLine < 1 || endLine < 1 || startLine > lineCount || endLine > lineCount || endLine < startLine {
			return nil
		}

		preview.fileContent = fileContent[startLine-1 : endLine]
		preview.subTitle = locale.Tr(
			"markup.filepreview.lines", startLine, endLine,
			template.HTML(commitLinkBuffer.String()),
		)

		preview.lineOffset = startLine - 1
	}

	return preview
}

func (p *FilePreview) CreateHTML(locale translation.Locale) *html.Node {
	table := &html.Node{
		Type: html.ElementNode,
		Data: atom.Table.String(),
		Attr: []html.Attribute{{Key: "class", Val: "file-preview"}},
	}
	tbody := &html.Node{
		Type: html.ElementNode,
		Data: atom.Tbody.String(),
	}

	status := &charset.EscapeStatus{}
	statuses := make([]*charset.EscapeStatus, len(p.fileContent))
	for i, line := range p.fileContent {
		statuses[i], p.fileContent[i] = charset.EscapeControlHTML(line, locale, charset.FileviewContext)
		status = status.Or(statuses[i])
	}

	for idx, code := range p.fileContent {
		tr := &html.Node{
			Type: html.ElementNode,
			Data: atom.Tr.String(),
		}

		lineNum := strconv.Itoa(p.lineOffset + idx + 1)

		tdLinesnum := &html.Node{
			Type: html.ElementNode,
			Data: atom.Td.String(),
			Attr: []html.Attribute{
				{Key: "class", Val: "lines-num"},
			},
		}
		spanLinesNum := &html.Node{
			Type: html.ElementNode,
			Data: atom.Span.String(),
			Attr: []html.Attribute{
				{Key: "data-line-number", Val: lineNum},
			},
		}
		tdLinesnum.AppendChild(spanLinesNum)
		tr.AppendChild(tdLinesnum)

		if status.Escaped {
			tdLinesEscape := &html.Node{
				Type: html.ElementNode,
				Data: atom.Td.String(),
				Attr: []html.Attribute{
					{Key: "class", Val: "lines-escape"},
				},
			}

			if statuses[idx].Escaped {
				btnTitle := ""
				if statuses[idx].HasInvisible {
					btnTitle += locale.TrString("repo.invisible_runes_line") + " "
				}
				if statuses[idx].HasAmbiguous {
					btnTitle += locale.TrString("repo.ambiguous_runes_line")
				}

				escapeBtn := &html.Node{
					Type: html.ElementNode,
					Data: atom.Button.String(),
					Attr: []html.Attribute{
						{Key: "class", Val: "toggle-escape-button btn interact-bg"},
						{Key: "title", Val: btnTitle},
					},
				}
				tdLinesEscape.AppendChild(escapeBtn)
			}

			tr.AppendChild(tdLinesEscape)
		}

		tdCode := &html.Node{
			Type: html.ElementNode,
			Data: atom.Td.String(),
			Attr: []html.Attribute{
				{Key: "class", Val: "lines-code chroma"},
			},
		}
		codeInner := &html.Node{
			Type: html.ElementNode,
			Data: atom.Code.String(),
			Attr: []html.Attribute{{Key: "class", Val: "code-inner"}},
		}
		codeText := &html.Node{
			Type: html.RawNode,
			Data: string(code),
		}
		codeInner.AppendChild(codeText)
		tdCode.AppendChild(codeInner)
		tr.AppendChild(tdCode)

		tbody.AppendChild(tr)
	}

	table.AppendChild(tbody)

	twrapper := &html.Node{
		Type: html.ElementNode,
		Data: atom.Div.String(),
		Attr: []html.Attribute{{Key: "class", Val: "ui table"}},
	}
	twrapper.AppendChild(table)

	header := &html.Node{
		Type: html.ElementNode,
		Data: atom.Div.String(),
		Attr: []html.Attribute{{Key: "class", Val: "header"}},
	}
	afilepath := &html.Node{
		Type: html.ElementNode,
		Data: atom.A.String(),
		Attr: []html.Attribute{
			{Key: "href", Val: p.urlFull},
			{Key: "class", Val: "muted"},
		},
	}
	afilepath.AppendChild(&html.Node{
		Type: html.TextNode,
		Data: p.filePath,
	})
	header.AppendChild(afilepath)

	psubtitle := &html.Node{
		Type: html.ElementNode,
		Data: atom.Span.String(),
		Attr: []html.Attribute{{Key: "class", Val: "text small grey"}},
	}
	psubtitle.AppendChild(&html.Node{
		Type: html.RawNode,
		Data: string(p.subTitle),
	})
	header.AppendChild(psubtitle)

	node := &html.Node{
		Type: html.ElementNode,
		Data: atom.Div.String(),
		Attr: []html.Attribute{{Key: "class", Val: "file-preview-box"}},
	}
	node.AppendChild(header)
	node.AppendChild(twrapper)

	return node
}
