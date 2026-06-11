package promtext

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"metriclens/backend/internal/model"
)

var (
	metricNamePattern = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	labelNamePattern  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

func Parse(r io.Reader) ([]model.MetricFamily, error) {
	parser := textParser{
		families: make(map[string]*model.MetricFamily),
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			if err := parser.parseComment(line); err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNumber, err)
			}
			continue
		}

		if err := parser.parseSample(line); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return parser.result(), nil
}

type textParser struct {
	families map[string]*model.MetricFamily
	order    []string
}

func (p *textParser) parseComment(line string) error {
	switch {
	case strings.HasPrefix(line, "# HELP "):
		return p.parseHelp(strings.TrimSpace(strings.TrimPrefix(line, "# HELP ")))
	case strings.HasPrefix(line, "# TYPE "):
		return p.parseType(strings.TrimSpace(strings.TrimPrefix(line, "# TYPE ")))
	default:
		return nil
	}
}

func (p *textParser) parseHelp(rest string) error {
	name, help, ok := strings.Cut(rest, " ")
	if !ok {
		name = rest
		help = ""
	}
	help = strings.TrimSpace(help)
	if !validMetricName(name) {
		return fmt.Errorf("invalid HELP metric name %q", name)
	}

	family := p.family(name)
	family.Help = help
	family.HasHelp = true
	return nil
}

func (p *textParser) parseType(rest string) error {
	fields := strings.Fields(rest)
	if len(fields) != 2 {
		return fmt.Errorf("invalid TYPE comment %q", rest)
	}
	if !validMetricName(fields[0]) {
		return fmt.Errorf("invalid TYPE metric name %q", fields[0])
	}

	metricType, err := parseMetricType(fields[1])
	if err != nil {
		return err
	}

	family := p.family(fields[0])
	if family.Type != model.MetricTypeUntyped && family.Type != metricType {
		return fmt.Errorf("conflicting TYPE for %q", fields[0])
	}
	family.Type = metricType
	family.HasType = true
	return nil
}

func (p *textParser) parseSample(line string) error {
	metricAndLabels, rest, err := splitSampleLine(line)
	if err != nil {
		return err
	}

	metricName, labels, err := parseMetricAndLabels(metricAndLabels)
	if err != nil {
		return err
	}

	fields := strings.Fields(rest)
	if len(fields) < 1 || len(fields) > 2 {
		return fmt.Errorf("invalid sample fields for %q", metricName)
	}

	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return fmt.Errorf("invalid sample value %q", fields[0])
	}

	var timestamp *int64
	if len(fields) == 2 {
		parsedTimestamp, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid sample timestamp %q", fields[1])
		}
		timestamp = &parsedTimestamp
	}

	family := p.family(p.familyNameForSample(metricName))
	family.Samples = append(family.Samples, model.MetricSample{
		Metric:    metricName,
		Labels:    labels,
		Value:     value,
		Timestamp: timestamp,
	})
	return nil
}

func (p *textParser) familyNameForSample(metricName string) string {
	if _, ok := p.families[metricName]; ok {
		return metricName
	}

	for _, suffix := range []string{"_bucket", "_sum", "_count"} {
		if !strings.HasSuffix(metricName, suffix) {
			continue
		}
		baseName := strings.TrimSuffix(metricName, suffix)
		family, ok := p.families[baseName]
		if ok && (family.Type == model.MetricTypeHistogram ||
			family.Type == model.MetricTypeSummary ||
			family.Type == model.MetricTypeUntyped) {
			return baseName
		}
	}

	return metricName
}

func (p *textParser) family(name string) *model.MetricFamily {
	family, ok := p.families[name]
	if ok {
		return family
	}

	family = &model.MetricFamily{
		Name: name,
		Type: model.MetricTypeUntyped,
	}
	p.families[name] = family
	p.order = append(p.order, name)
	return family
}

func (p *textParser) result() []model.MetricFamily {
	families := make([]model.MetricFamily, 0, len(p.order))
	for _, name := range p.order {
		families = append(families, *p.families[name])
	}
	return families
}

func splitSampleLine(line string) (string, string, error) {
	openBrace := strings.IndexByte(line, '{')
	firstSpace := strings.IndexAny(line, " \t")

	if openBrace >= 0 && (firstSpace == -1 || openBrace < firstSpace) {
		closeBrace := strings.IndexByte(line[openBrace:], '}')
		if closeBrace == -1 {
			return "", "", fmt.Errorf("unterminated label set")
		}
		tokenEnd := openBrace + closeBrace + 1
		if tokenEnd >= len(line) || !isSpace(line[tokenEnd]) {
			return "", "", fmt.Errorf("missing whitespace after label set")
		}
		return line[:tokenEnd], strings.TrimSpace(line[tokenEnd:]), nil
	}

	if firstSpace == -1 {
		return "", "", fmt.Errorf("sample missing value")
	}
	return line[:firstSpace], strings.TrimSpace(line[firstSpace:]), nil
}

func parseMetricAndLabels(value string) (string, map[string]string, error) {
	openBrace := strings.IndexByte(value, '{')
	if openBrace == -1 {
		if !validMetricName(value) {
			return "", nil, fmt.Errorf("invalid metric name %q", value)
		}
		return value, map[string]string{}, nil
	}

	if !strings.HasSuffix(value, "}") {
		return "", nil, fmt.Errorf("invalid label set")
	}

	metricName := value[:openBrace]
	if !validMetricName(metricName) {
		return "", nil, fmt.Errorf("invalid metric name %q", metricName)
	}

	labels, err := parseLabels(value[openBrace+1 : len(value)-1])
	if err != nil {
		return "", nil, err
	}
	return metricName, labels, nil
}

func parseLabels(value string) (map[string]string, error) {
	labels := map[string]string{}
	i := 0
	for i < len(value) {
		i = skipSpaces(value, i)
		nameStart := i
		for i < len(value) && value[i] != '=' {
			i++
		}
		if i == nameStart || i >= len(value) {
			return nil, fmt.Errorf("invalid label")
		}

		name := strings.TrimSpace(value[nameStart:i])
		if !labelNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid label name %q", name)
		}
		if _, ok := labels[name]; ok {
			return nil, fmt.Errorf("duplicate label %q", name)
		}

		i++
		i = skipSpaces(value, i)
		if i >= len(value) || value[i] != '"' {
			return nil, fmt.Errorf("label %q value is not quoted", name)
		}

		rawValue, next, err := readQuoted(value, i)
		if err != nil {
			return nil, fmt.Errorf("invalid label %q value: %w", name, err)
		}
		labelValue, err := strconv.Unquote(rawValue)
		if err != nil {
			return nil, fmt.Errorf("invalid label %q value: %w", name, err)
		}
		labels[name] = labelValue

		i = skipSpaces(value, next)
		if i == len(value) {
			break
		}
		if value[i] != ',' {
			return nil, fmt.Errorf("expected comma after label %q", name)
		}
		i++
		if skipSpaces(value, i) == len(value) {
			return nil, fmt.Errorf("trailing comma after label %q", name)
		}
	}
	return labels, nil
}

func readQuoted(value string, start int) (string, int, error) {
	escaped := false
	for i := start + 1; i < len(value); i++ {
		switch {
		case escaped:
			escaped = false
		case value[i] == '\\':
			escaped = true
		case value[i] == '"':
			return value[start : i+1], i + 1, nil
		}
	}
	return "", 0, fmt.Errorf("unterminated quoted string")
}

func parseMetricType(value string) (model.MetricType, error) {
	switch model.MetricType(value) {
	case model.MetricTypeCounter,
		model.MetricTypeGauge,
		model.MetricTypeHistogram,
		model.MetricTypeSummary,
		model.MetricTypeUntyped:
		return model.MetricType(value), nil
	default:
		return "", fmt.Errorf("invalid metric type %q", value)
	}
}

func validMetricName(name string) bool {
	return metricNamePattern.MatchString(name)
}

func skipSpaces(value string, i int) int {
	for i < len(value) && isSpace(value[i]) {
		i++
	}
	return i
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t'
}
