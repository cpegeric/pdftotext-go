package pdftotext

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"reflect"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Extract PDF text content in simplified format
func Extract(pdfBytes []byte) (pdfPages []PdfPage, err error) {
	var tsv []PopplerTsvRow
	if tsv, err = ExtractInPopplerTsv(pdfBytes); err != nil {
		return nil, err
	}

	prevPage := 1
	prevContent := ""
	for i, row := range tsv {
		if row.Conf != -1 { // Seems to indicate control sequences
			prevContent += row.Text + " "
		}

		var pageChanged = prevPage != row.PageNum
		var lastIteration = i == len(tsv)-1

		if pageChanged || lastIteration {
			pdfPages = append(pdfPages, PdfPage{
				Content: prevContent,
				Number:  prevPage,
			})

			prevPage = row.PageNum
			prevContent = ""
		}

	}

	return pdfPages, nil
}

// ExtractOrError Just like Extract, but indicates issues with errors
func ExtractOrError(pdfBytes []byte) (pages []PdfPage, err error) {
	if pages, err = Extract(pdfBytes); err != nil {
		return pages, err
	}

	if len(pages) == 0 {
		return pages, fmt.Errorf("no pages extracted")
	}

	hasContent := false
	for _, p := range pages {
		if p.Content != "" {
			hasContent = true
			break
		}
	}

	if !hasContent {
		return pages, fmt.Errorf("no page text extracted")
	}

	return pages, err
}

// ExtractInPopplerTsv Access raw stdout content from Poppler
func ExtractInPopplerTsv(pdfBytes []byte) (tsvRows []PopplerTsvRow, err error) {
	params := []string{
		"-tsv",
		"-", // Read from stdin
		"-", // Write to stdout
	}

	cmd := exec.Command("pdftotext", params...)
	cmd.Stdin = bytes.NewReader(pdfBytes)

	var out bytes.Buffer
	cmd.Stdout = &out

	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("error executing pdftotext binary: %w", err)
	}

	tsvT := reflect.TypeOf(PopplerTsvRow{})
	scanner := bufio.NewScanner(strings.NewReader(string(out.Bytes())))

	scanner.Scan() // Ignore TSV header
	for scanner.Scan() {
		var (
			line   = scanner.Text()
			fields = strings.Fields(line)
		)

		newTsv := PopplerTsvRow{}

		for i := 0; i < tsvT.NumField(); i++ {
			if i >= len(fields) {
				continue
			}

			field := reflect.ValueOf(&newTsv).Elem().Field(i)
			var col int
			if col, err = strconv.Atoi(tsvT.Field(i).Tag.Get("col")); err != nil {
				return nil, fmt.Errorf(string("cannot parse tag as int: %w"), err)
			}

			switch field.Interface().(type) {
			case int:
				var newInteger int
				if newInteger, err = strconv.Atoi(fields[col]); err != nil {
					return nil, fmt.Errorf("cannot convert value to int: %w", err)
				}
				field.SetInt(int64(newInteger))
			case float64:
				var newFloat float64
				if newFloat, err = strconv.ParseFloat(fields[col], 64); err != nil {
					return nil, fmt.Errorf("cannot convert value to float32: %w", err)
				}
				field.SetFloat(newFloat)
				break
			case string:
				field.SetString(fields[col])
			default:
				return nil, fmt.Errorf("don't know how to map " + field.Type().String())
			}
		}

		tsvRows = append(tsvRows, newTsv)
	}

	return tsvRows, nil
}

func CheckPopplerVersion() (fullVersionString string, err error) {
	cmd := exec.Command("pdftotext", "-v")

	var out bytes.Buffer
	cmd.Stderr = &out

	if err = cmd.Run(); err != nil {
		return "", fmt.Errorf("error executing binary: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out.Bytes())))

	scanner.Scan()
	line := scanner.Text()
	fields := strings.Fields(line)

	if len(fields) < 2 {
		return "", fmt.Errorf("No version information extracted")
	}

	fullVersionString = fields[2]

	var constraint *semver.Constraints
	var version *semver.Version

	// https://poppler.freedesktop.org/releases.html
	// poppler introduced the "tsv" parameter with this version:
	const popplerVersionConstraint = ">= 22.05.0"

	if constraint, err = semver.NewConstraint(popplerVersionConstraint); err != nil {
		return "", fmt.Errorf("Cannot parse constraint string \"%s\"", popplerVersionConstraint)
	}

	if version, err = semver.NewVersion(fullVersionString); err != nil {
		return "", fmt.Errorf("Cannot parse version string \"%s\"", fullVersionString)
	}

	if constraint.Check(version) {
		// poppler is compatible
		return fullVersionString, nil
	}

	return "", fmt.Errorf("Incompatible poppler version: require version \"%s\", but found version \"%s\"", constraint.String(), version.String())
}
