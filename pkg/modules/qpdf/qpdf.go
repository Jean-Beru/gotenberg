package qpdf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/gotenberg/gotenberg/v8/pkg/gotenberg"
)

func init() {
	gotenberg.MustRegisterModule(new(QPdf))
}

// QPdf abstracts the CLI tool QPDF and implements the [gotenberg.PdfEngine]
// interface.
type QPdf struct {
	binPath    string
	globalArgs []string
}

// Descriptor returns a [QPdf]'s module descriptor.
func (engine *QPdf) Descriptor() gotenberg.ModuleDescriptor {
	return gotenberg.ModuleDescriptor{
		ID:  "qpdf",
		New: func() gotenberg.Module { return new(QPdf) },
	}
}

// Provision sets the module properties.
func (engine *QPdf) Provision(ctx *gotenberg.Context) error {
	binPath, ok := os.LookupEnv("QPDF_BIN_PATH")
	if !ok {
		return errors.New("QPDF_BIN_PATH environment variable is not set")
	}

	engine.binPath = binPath
	// Warnings should not cause errors.
	engine.globalArgs = []string{"--warning-exit-0", "--newline-before-endstream"}

	return nil
}

// Validate validates the module properties.
func (engine *QPdf) Validate() error {
	_, err := os.Stat(engine.binPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("QPDF binary path does not exist: %w", err)
	}

	return nil
}

// Debug returns additional debug data.
func (engine *QPdf) Debug() map[string]any {
	debug := make(map[string]any)

	cmd := exec.Command(engine.binPath, "--version") //nolint:gosec
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	output, err := cmd.Output()
	if err != nil {
		debug["version"] = err.Error()
		return debug
	}

	lines := bytes.SplitN(output, []byte("\n"), 2)
	if len(lines) > 0 {
		debug["version"] = string(lines[0])
	} else {
		debug["version"] = "Unable to determine QPDF version"
	}

	return debug
}

// Split splits a given PDF file.
func (engine *QPdf) Split(ctx context.Context, logger *slog.Logger, mode gotenberg.SplitMode, inputPath, outputDirPath string) ([]string, error) {
	ctx, span := gotenberg.Tracer().Start(ctx, "qpdf.Split",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	var args []string
	outputPath := fmt.Sprintf("%s/%s", outputDirPath, filepath.Base(inputPath))

	switch mode.Mode {
	case gotenberg.SplitModePages:
		if !mode.Unify {
			err := fmt.Errorf("split PDFs using mode '%s' without unify with QPDF: %w", mode.Mode, gotenberg.ErrPdfSplitModeNotSupported)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		args = append(args, inputPath)
		args = append(args, engine.globalArgs...)
		args = append(args, "--pages", ".", mode.Span)
		args = append(args, "--", outputPath)
	default:
		err := fmt.Errorf("split PDFs using mode '%s' with QPDF: %w", mode.Mode, gotenberg.ErrPdfSplitModeNotSupported)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	cmd, err := gotenberg.CommandContext(ctx, logger, engine.binPath, args...)
	if err != nil {
		err = fmt.Errorf("create command: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	_, err = cmd.Exec()
	if err != nil {
		err = fmt.Errorf("split PDFs with QPDF: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetStatus(codes.Ok, "")
	return []string{outputPath}, nil
}

// Merge combines multiple PDFs into a single PDF.
func (engine *QPdf) Merge(ctx context.Context, logger *slog.Logger, inputPaths []string, outputPath string) error {
	ctx, span := gotenberg.Tracer().Start(ctx, "qpdf.Merge",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	args := make([]string, 0, 4+len(engine.globalArgs)+len(inputPaths))
	args = append(args, "--empty")
	args = append(args, engine.globalArgs...)
	args = append(args, "--pages")
	args = append(args, inputPaths...)
	args = append(args, "--", outputPath)

	cmd, err := gotenberg.CommandContext(ctx, logger, engine.binPath, args...)
	if err != nil {
		err = fmt.Errorf("create command: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	_, err = cmd.Exec()
	if err == nil {
		span.SetStatus(codes.Ok, "")
		return nil
	}

	err = fmt.Errorf("merge PDFs with QPDF: %w", err)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// Flatten merges annotation appearances with page content, deleting the
// original annotations.
func (engine *QPdf) Flatten(ctx context.Context, logger *slog.Logger, inputPath string) error {
	ctx, span := gotenberg.Tracer().Start(ctx, "qpdf.Flatten",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	args := make([]string, 0, 4+len(engine.globalArgs))
	args = append(args, inputPath)
	args = append(args, "--generate-appearances")
	args = append(args, "--flatten-annotations=all")
	args = append(args, "--replace-input")
	args = append(args, engine.globalArgs...)

	cmd, err := gotenberg.CommandContext(ctx, logger, engine.binPath, args...)
	if err != nil {
		err = fmt.Errorf("create command: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	_, err = cmd.Exec()
	if err == nil {
		span.SetStatus(codes.Ok, "")
		return nil
	}

	err = fmt.Errorf("flatten PDFs with QPDF: %w", err)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// Convert is not available in this implementation.
func (engine *QPdf) Convert(ctx context.Context, logger *slog.Logger, formats gotenberg.PdfFormats, inputPath, outputPath string) error {
	_, span := gotenberg.Tracer().Start(ctx, "qpdf.Convert",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	err := fmt.Errorf("convert PDF to '%+v' with QPDF: %w", formats, gotenberg.ErrPdfEngineMethodNotSupported)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// ReadMetadata is not available in this implementation.
func (engine *QPdf) ReadMetadata(ctx context.Context, logger *slog.Logger, inputPath string) (map[string]any, error) {
	_, span := gotenberg.Tracer().Start(ctx, "qpdf.ReadMetadata",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	err := fmt.Errorf("read PDF metadata with QPDF: %w", gotenberg.ErrPdfEngineMethodNotSupported)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return nil, err
}

// WriteMetadata is not available in this implementation.
func (engine *QPdf) WriteMetadata(ctx context.Context, logger *slog.Logger, metadata map[string]any, inputPath string) error {
	_, span := gotenberg.Tracer().Start(ctx, "qpdf.WriteMetadata",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	err := fmt.Errorf("write PDF metadata with QPDF: %w", gotenberg.ErrPdfEngineMethodNotSupported)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// PageCount is not available in this implementation.
func (engine *QPdf) PageCount(ctx context.Context, logger *slog.Logger, inputPath string) (int, error) {
	_, span := gotenberg.Tracer().Start(ctx, "qpdf.PageCount",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	err := fmt.Errorf("page count with QPDF: %w", gotenberg.ErrPdfEngineMethodNotSupported)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return 0, err
}

// WriteBookmarks is not available in this implementation.
func (engine *QPdf) WriteBookmarks(ctx context.Context, logger *slog.Logger, inputPath string, bookmarks []gotenberg.Bookmark) error {
	_, span := gotenberg.Tracer().Start(ctx, "qpdf.WriteBookmarks",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	err := fmt.Errorf("write PDF bookmarks with QPDF: %w", gotenberg.ErrPdfEngineMethodNotSupported)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// ReadBookmarks is not available in this implementation.
func (engine *QPdf) ReadBookmarks(ctx context.Context, logger *slog.Logger, inputPath string) ([]gotenberg.Bookmark, error) {
	_, span := gotenberg.Tracer().Start(ctx, "qpdf.ReadBookmarks",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	err := fmt.Errorf("read PDF bookmarks with QPDF: %w", gotenberg.ErrPdfEngineMethodNotSupported)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return nil, err
}

// Encrypt adds password protection to a PDF file using QPDF.
func (engine *QPdf) Encrypt(ctx context.Context, logger *slog.Logger, inputPath, userPassword, ownerPassword string) error {
	ctx, span := gotenberg.Tracer().Start(ctx, "qpdf.Encrypt",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	if userPassword == "" {
		err := errors.New("user password cannot be empty")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	if ownerPassword == "" {
		ownerPassword = userPassword
	}

	args := make([]string, 0, 7+len(engine.globalArgs))
	args = append(args, inputPath)
	args = append(args, engine.globalArgs...)
	args = append(args, "--replace-input")
	args = append(args, "--encrypt", userPassword, ownerPassword, "256", "--")

	cmd, err := gotenberg.CommandContext(ctx, logger, engine.binPath, args...)
	if err != nil {
		err = fmt.Errorf("create command: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	_, err = cmd.Exec()
	if err != nil {
		err = fmt.Errorf("encrypt PDF with QPDF: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// EmbedFiles embeds files into a PDF using QPDF's --add-attachment flag.
func (engine *QPdf) EmbedFiles(ctx context.Context, logger *slog.Logger, filePaths []string, inputPath string) error {
	ctx, span := gotenberg.Tracer().Start(ctx, "qpdf.EmbedFiles",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	if len(filePaths) == 0 {
		span.SetStatus(codes.Ok, "")
		return nil
	}

	logger.Debug(fmt.Sprintf("embedding %d file(s) to %s with QPDF: %v", len(filePaths), inputPath, filePaths))

	args := make([]string, 0, 2+len(engine.globalArgs)+3*len(filePaths))
	args = append(args, inputPath)
	args = append(args, engine.globalArgs...)
	for _, fp := range filePaths {
		args = append(args, "--add-attachment", fp, "--")
	}
	args = append(args, "--replace-input")

	cmd, err := gotenberg.CommandContext(ctx, logger, engine.binPath, args...)
	if err != nil {
		err = fmt.Errorf("create command for embedding files: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	_, err = cmd.Exec()
	if err != nil {
		err = fmt.Errorf("embed files with QPDF: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// EmbedFilesMetadata sets metadata on already-embedded files in a PDF using
// QPDF's JSON manipulation. It sets /AFRelationship on Filespec objects,
// /Subtype on EmbeddedFile streams, and ensures the Catalog /AF array
// references the Filespec objects.
func (engine *QPdf) EmbedFilesMetadata(ctx context.Context, logger *slog.Logger, metadata map[string]map[string]string, inputPath string) error {
	ctx, span := gotenberg.Tracer().Start(ctx, "qpdf.EmbedFilesMetadata",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	if len(metadata) == 0 {
		span.SetStatus(codes.Ok, "")
		return nil
	}

	logger.Debug(fmt.Sprintf("setting embeds metadata on %s with QPDF", inputPath))

	// Step 1: Get JSON representation of the PDF.
	jsonArgs := append([]string{inputPath}, engine.globalArgs...)
	jsonArgs = append(jsonArgs, "--json-output")

	jsonCmd := exec.CommandContext(ctx, engine.binPath, jsonArgs...) //nolint:gosec
	jsonCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	output, err := jsonCmd.Output()
	if err != nil {
		err = fmt.Errorf("get PDF JSON with QPDF: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Step 2: Parse the QPDF JSON v2 structure.
	var pdfJSON struct {
		Qpdf []json.RawMessage `json:"qpdf"`
	}
	if err := json.Unmarshal(output, &pdfJSON); err != nil {
		err = fmt.Errorf("parse PDF JSON: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if len(pdfJSON.Qpdf) < 2 {
		err = fmt.Errorf("unexpected QPDF JSON structure: expected at least 2 elements")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	var objects map[string]json.RawMessage
	if err := json.Unmarshal(pdfJSON.Qpdf[1], &objects); err != nil {
		err = fmt.Errorf("parse QPDF objects: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Step 3: Walk objects to find Filespecs and the Catalog.
	updateObjects := make(map[string]any)
	var catalogRef string
	var catalogValue map[string]any
	var filespecRefs []string

	for ref, raw := range objects {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}

		// Check if this is a "value" object (not a stream).
		valueRaw, hasValue := obj["value"]
		streamRaw, hasStream := obj["stream"]

		if hasValue {
			var value map[string]any
			if err := json.Unmarshal(valueRaw, &value); err != nil {
				continue
			}

			typeVal, _ := value["/Type"].(string)

			if typeVal == "/Catalog" {
				catalogRef = ref
				catalogValue = value
			}

			if typeVal == "/Filespec" {
				uf, _ := value["/UF"].(string)
				if uf == "" {
					uf, _ = value["/F"].(string)
				}

				// QPDF JSON encodes strings with a type prefix
				// (e.g., "u:factur-x.xml" for Unicode). Strip it
				// so the lookup matches the plain filename from
				// the form metadata.
				cleanUf := stripQpdfStringPrefix(uf)

				meta, exists := metadata[cleanUf]
				if !exists {
					continue
				}

				if rel, ok := meta["relationship"]; ok {
					value["/AFRelationship"] = "/" + rel
				}

				// Set /Subtype on the EmbeddedFile stream.
				if mimeType, ok := meta["mimeType"]; ok {
					if ef, ok := value["/EF"].(map[string]any); ok {
						efRef, _ := ef["/F"].(string)
						if efRef != "" {
							engine.setStreamSubtype(objects, updateObjects, efRef, mimeType)
						}
					}
				}

				filespecRefs = append(filespecRefs, ref)
				updateObjects[ref] = map[string]any{"value": value}
			}
		} else if hasStream {
			// Streams are handled when referenced from Filespecs.
			_ = streamRaw
		}
	}

	if len(filespecRefs) == 0 {
		span.SetStatus(codes.Ok, "")
		return nil
	}

	// Step 4: Ensure Catalog /AF array references the Filespec objects.
	if catalogRef != "" && catalogValue != nil {
		afSet := make(map[string]bool)
		existingAF, _ := catalogValue["/AF"].([]any)
		for _, r := range existingAF {
			if s, ok := r.(string); ok {
				afSet[s] = true
			}
		}
		for _, ref := range filespecRefs {
			// Object references in values use "9 0 R" format,
			// not the "obj:9 0 R" key format.
			valRef := strings.TrimPrefix(ref, "obj:")
			if !afSet[valRef] {
				existingAF = append(existingAF, valRef)
			}
		}
		catalogValue["/AF"] = existingAF
		updateObjects[catalogRef] = map[string]any{"value": catalogValue}
	}

	// Step 5: Build and write update JSON.
	updateJSON := map[string]any{
		"qpdf": []any{
			map[string]any{
				"jsonversion":                  2,
				"pushedinheritedpageresources": false,
				"calledgetallpages":            false,
				"maxobjectid":                  0,
			},
			updateObjects,
		},
	}

	jsonBytes, err := json.Marshal(updateJSON)
	if err != nil {
		err = fmt.Errorf("marshal update JSON: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	tmpFile := filepath.Join(filepath.Dir(inputPath), "qpdf-embeds-metadata.json")
	if err := os.WriteFile(tmpFile, jsonBytes, 0600); err != nil {
		err = fmt.Errorf("write update JSON: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	defer os.Remove(tmpFile)

	// Step 6: Apply the update.
	updateArgs := make([]string, 0, 4+len(engine.globalArgs))
	updateArgs = append(updateArgs, inputPath)
	updateArgs = append(updateArgs, engine.globalArgs...)
	updateArgs = append(updateArgs, "--update-from-json="+tmpFile)
	updateArgs = append(updateArgs, "--replace-input")

	cmd, err := gotenberg.CommandContext(ctx, logger, engine.binPath, updateArgs...)
	if err != nil {
		err = fmt.Errorf("create command for JSON update: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	_, err = cmd.Exec()
	if err != nil {
		err = fmt.Errorf("update embeds metadata with QPDF: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// setStreamSubtype finds a stream object by reference and sets the /Subtype
// key in its dict. The MIME type is encoded as a PDF name (e.g., "text/xml"
// becomes "/text#2fxml").
func (engine *QPdf) setStreamSubtype(objects map[string]json.RawMessage, updateObjects map[string]any, ref, mimeType string) {
	// Object keys in the QPDF JSON use "obj:" prefix.
	objKey := ref
	if !strings.HasPrefix(objKey, "obj:") {
		objKey = "obj:" + objKey
	}
	raw, ok := objects[objKey]
	if !ok {
		return
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return
	}

	streamRaw, ok := obj["stream"]
	if !ok {
		return
	}

	var stream map[string]any
	if err := json.Unmarshal(streamRaw, &stream); err != nil {
		return
	}

	dict, ok := stream["dict"].(map[string]any)
	if !ok {
		return
	}

	// QPDF JSON uses literal name syntax; it handles PDF name
	// encoding internally when writing the binary PDF.
	dict["/Subtype"] = "/" + mimeType
	stream["dict"] = dict
	updateObjects[objKey] = map[string]any{"stream": stream}
}

// stripQpdfStringPrefix removes the type prefix that QPDF adds to JSON
// string values (e.g., "u:" for Unicode, "b:" for binary).
func stripQpdfStringPrefix(s string) string {
	if idx := strings.Index(s, ":"); idx >= 0 && idx <= 2 {
		return s[idx+1:]
	}
	return s
}

// Watermark is not available in this implementation.
func (engine *QPdf) Watermark(ctx context.Context, logger *slog.Logger, inputPath string, stamp gotenberg.Stamp) error {
	_, span := gotenberg.Tracer().Start(ctx, "qpdf.Watermark",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	err := fmt.Errorf("watermark PDF with QPDF: %w", gotenberg.ErrPdfEngineMethodNotSupported)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// Stamp is not available in this implementation.
func (engine *QPdf) Stamp(ctx context.Context, logger *slog.Logger, inputPath string, stamp gotenberg.Stamp) error {
	_, span := gotenberg.Tracer().Start(ctx, "qpdf.Stamp",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	err := fmt.Errorf("stamp PDF with QPDF: %w", gotenberg.ErrPdfEngineMethodNotSupported)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// Rotate is not available in this implementation.
func (engine *QPdf) Rotate(ctx context.Context, logger *slog.Logger, inputPath string, angle int, pages string) error {
	_, span := gotenberg.Tracer().Start(ctx, "qpdf.Rotate",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(engine.binPath)),
	)
	defer span.End()

	err := fmt.Errorf("rotate PDF with QPDF: %w", gotenberg.ErrPdfEngineMethodNotSupported)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

var (
	_ gotenberg.Module      = (*QPdf)(nil)
	_ gotenberg.Provisioner = (*QPdf)(nil)
	_ gotenberg.Validator   = (*QPdf)(nil)
	_ gotenberg.Debuggable  = (*QPdf)(nil)
	_ gotenberg.PdfEngine   = (*QPdf)(nil)
)
