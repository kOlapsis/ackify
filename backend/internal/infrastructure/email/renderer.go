// SPDX-License-Identifier: AGPL-3.0-or-later
package email

import (
	"bytes"
	"fmt"
	htmlTemplate "html/template"
	"os"
	"path/filepath"
	"strings"
	txtTemplate "text/template"

	"github.com/kolapsis/ackify/backend/internal/infrastructure/i18n"
)

type Renderer struct {
	templateDir   string
	baseURL       string
	organisation  string
	fromName      string
	fromMail      string
	defaultLocale string
	i18n          *i18n.I18n
}

type TemplateData struct {
	Organisation string
	BaseURL      string
	FromName     string
	FromMail     string
	Data         map[string]any
	T            func(key string, args ...map[string]any) string
}

func NewRenderer(templateDir, baseURL, organisation, fromName, fromMail, defaultLocale string, i18nBundle *i18n.I18n) *Renderer {
	return &Renderer{
		templateDir:   templateDir,
		baseURL:       baseURL,
		organisation:  organisation,
		fromName:      fromName,
		fromMail:      fromMail,
		defaultLocale: defaultLocale,
		i18n:          i18nBundle,
	}
}

func (r *Renderer) Render(templateName, locale string, data map[string]any) (htmlBody, textBody string, err error) {
	if locale == "" {
		locale = r.defaultLocale
	}

	// Create translation function with template variable interpolation
	tFunc := func(key string, args ...map[string]any) string {
		translated := r.i18n.T(locale, key)

		// If args provided, interpolate {{.VarName}} placeholders
		if len(args) > 0 && args[0] != nil {
			for k, v := range args[0] {
				placeholder := fmt.Sprintf("{{.%s}}", k)
				translated = strings.ReplaceAll(translated, placeholder, fmt.Sprintf("%v", v))
			}
		}

		return translated
	}

	templateData := TemplateData{
		Organisation: r.organisation,
		BaseURL:      r.baseURL,
		FromName:     r.fromName,
		FromMail:     r.fromMail,
		Data:         data,
		T:            tFunc,
	}

	htmlBody, err = r.renderHTML(templateName, locale, templateData)
	if err != nil {
		return "", "", fmt.Errorf("failed to render HTML: %w", err)
	}

	textBody, err = r.renderText(templateName, locale, templateData)
	if err != nil {
		return "", "", fmt.Errorf("failed to render text: %w", err)
	}

	return htmlBody, textBody, nil
}

func (r *Renderer) renderHTML(templateName, locale string, data TemplateData) (string, error) {
	baseTemplatePath := filepath.Join(r.templateDir, "base.html.tmpl")
	templatePath := filepath.Join(r.templateDir, fmt.Sprintf("%s.html.tmpl", templateName))

	if _, err := os.Stat(templatePath); err != nil {
		return "", fmt.Errorf("template not found: %s", templatePath)
	}

	// Create template with helper functions
	tmpl := htmlTemplate.New("base").Funcs(htmlTemplate.FuncMap{
		"dict": func(args ...interface{}) map[string]any {
			if len(args)%2 != 0 {
				return nil
			}
			dict := make(map[string]any)
			for i := 0; i < len(args); i += 2 {
				key, ok := args[i].(string)
				if !ok {
					continue
				}
				dict[key] = args[i+1]
			}
			return dict
		},
		"T": data.T, // Expose T function to template
	})

	tmpl, err := tmpl.ParseFiles(baseTemplatePath, templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

func (r *Renderer) renderText(templateName, locale string, data TemplateData) (string, error) {
	baseTemplatePath := filepath.Join(r.templateDir, "base.txt.tmpl")
	templatePath := filepath.Join(r.templateDir, fmt.Sprintf("%s.txt.tmpl", templateName))

	if _, err := os.Stat(templatePath); err != nil {
		return "", fmt.Errorf("template not found: %s", templatePath)
	}

	// Create template with helper functions
	tmpl := txtTemplate.New("base").Funcs(txtTemplate.FuncMap{
		"dict": func(args ...interface{}) map[string]any {
			if len(args)%2 != 0 {
				return nil
			}
			dict := make(map[string]any)
			for i := 0; i < len(args); i += 2 {
				key, ok := args[i].(string)
				if !ok {
					continue
				}
				dict[key] = args[i+1]
			}
			return dict
		},
		"T": data.T, // Expose T function to template
	})

	tmpl, err := tmpl.ParseFiles(baseTemplatePath, templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}
