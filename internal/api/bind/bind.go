// Package bind decodes and validates JSON request bodies. It is the one
// place decode-strictness and validation-tag-to-message translation happen,
// so handlers stay free of hand-rolled if/else validation.
package bind

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
)

// validate is a single, cached validator instance — the package doc for
// go-playground/validator recommends this over constructing one per call.
var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	// Field names in error messages should match the JSON wire format, not
	// the Go struct field name.
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "" || name == "-" {
			return fld.Name
		}
		return name
	})
	return v
}

// JSON decodes r's body into a T and validates it against T's `validate`
// struct tags. It rejects bodies with unknown fields or trailing data after
// the first JSON value. Any failure is returned as a *problem.Problem ready
// to propagate straight out of a problem.HandlerFunc.
func JSON[T any](r *http.Request) (T, error) {
	var v T

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, decodeError(err)
	}
	// A second Decode call must hit EOF — anything else means there was more
	// than one JSON value in the body.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return v, problem.BadRequest("body JSON chứa dữ liệu thừa")
	}

	if err := validate.Struct(v); err != nil {
		return v, validationError(err)
	}

	return v, nil
}

func decodeError(err error) *problem.Problem {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return problem.TooLarge()
	}

	if errors.Is(err, io.EOF) {
		return problem.BadRequest("body JSON không được để trống")
	}

	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return problem.BadRequest("body JSON không hợp lệ")
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return problem.BadRequest(fmt.Sprintf("trường %q có kiểu dữ liệu không hợp lệ", typeErr.Field))
	}

	// encoding/json has no exported type for "unknown field" (produced by
	// DisallowUnknownFields); it only surfaces as a formatted string.
	if strings.HasPrefix(err.Error(), "json: unknown field ") {
		return problem.BadRequest("body JSON chứa trường không hợp lệ")
	}

	return problem.BadRequest("body JSON không hợp lệ")
}

func validationError(err error) *problem.Problem {
	var fieldErrs validator.ValidationErrors
	if !errors.As(err, &fieldErrs) {
		return problem.BadRequest("dữ liệu không hợp lệ")
	}

	msgs := make([]string, 0, len(fieldErrs))
	for _, fe := range fieldErrs {
		msgs = append(msgs, fieldMessage(fe))
	}
	return problem.BadRequest(strings.Join(msgs, "; "))
}

// fieldMessage translates one validator tag failure into a Vietnamese,
// user-facing message. Extend this switch as new validate tags are adopted.
func fieldMessage(fe validator.FieldError) string {
	field := fe.Field()
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s không được để trống", field)
	case "max":
		return fmt.Sprintf("%s không được vượt quá %s ký tự", field, fe.Param())
	case "min":
		return fmt.Sprintf("%s phải có ít nhất %s ký tự", field, fe.Param())
	default:
		return fmt.Sprintf("%s không hợp lệ", field)
	}
}
