package db

import (
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"time"
)

// TimeFormat is how every time is kept: UTC, nanoseconds always written, so
// times of one width compare as text.
const TimeFormat = "2006-01-02T15:04:05.000000000Z"

// Time is t as it's kept.
func Time(t time.Time) string { return t.UTC().Format(TimeFormat) }

// Earliest sorts before every time: "now, before anything else".
var Earliest = Time(time.Time{})

var timeType = reflect.TypeOf(time.Time{})

// bind converts args to what SQLite keeps.
func bind(args []any) ([]any, error) {
	out := make([]any, len(args))
	for i, a := range args {
		v, err := value(a)
		if err != nil {
			return nil, fmt.Errorf("argument $%d: %w", i+1, err)
		}
		out[i] = v
	}
	return out, nil
}

func value(a any) (any, error) {
	switch v := a.(type) {
	case nil:
		return nil, nil
	case time.Time:
		return Time(v), nil
	case *time.Time:
		if v == nil {
			return nil, nil
		}
		return Time(*v), nil
	case []float32:
		if v == nil {
			return nil, nil
		}
		return vector(v), nil
	case []string:
		if v == nil {
			v = []string{}
		}
		b, err := json.Marshal(v)
		return string(b), err
	case json.RawMessage:
		if v == nil {
			return nil, nil
		}
		return string(v), nil
	case []byte, string, bool, int, int32, int64, float32, float64:
		return v, nil
	case driver.Valuer:
		return v.Value()
	}
	rv := reflect.ValueOf(a)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, nil
		}
		return value(rv.Elem().Interface())
	}
	switch rv.Kind() {
	case reflect.Map, reflect.Struct, reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Map && rv.IsNil() {
			return "{}", nil
		}
		b, err := json.Marshal(a)
		return string(b), err
	case reflect.String:
		return rv.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(rv.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return rv.Float(), nil
	case reflect.Bool:
		return rv.Bool(), nil
	}
	return nil, fmt.Errorf("can't keep a %T", a)
}

func vector(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

// scanInto scans with scan, reading each column into dest as bind wrote it.
func scanInto(scan func(...any) error, dest []any) error {
	raw := make([]any, len(dest))
	for i := range raw {
		raw[i] = new(any)
	}
	if err := scan(raw...); err != nil {
		return err
	}
	for i, d := range dest {
		if err := assign(d, *(raw[i].(*any))); err != nil {
			return fmt.Errorf("column %d: %w", i, err)
		}
	}
	return nil
}

func assign(dest, src any) error {
	if s, ok := dest.(sql.Scanner); ok {
		return s.Scan(src)
	}
	switch d := dest.(type) {
	case *any:
		*d = src
		return nil
	case *time.Time:
		t, err := parseTime(src)
		*d = t
		return err
	case **time.Time:
		if src == nil {
			*d = nil
			return nil
		}
		t, err := parseTime(src)
		*d = &t
		return err
	case *[]float32:
		b, ok := src.([]byte)
		if src == nil || !ok || len(b) == 0 {
			*d = nil
			return nil
		}
		v := make([]float32, len(b)/4)
		for i := range v {
			v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
		}
		*d = v
		return nil
	case *json.RawMessage:
		switch s := src.(type) {
		case nil:
			*d = nil
		case string:
			*d = json.RawMessage(s)
		case []byte:
			*d = append(json.RawMessage(nil), s...)
		default:
			return fmt.Errorf("can't read %T as JSON", src)
		}
		return nil
	case *[]byte:
		switch s := src.(type) {
		case nil:
			*d = nil
		case string:
			*d = []byte(s)
		case []byte:
			*d = append([]byte(nil), s...)
		default:
			return fmt.Errorf("can't read %T as bytes", src)
		}
		return nil
	case *bool:
		switch s := src.(type) {
		case int64:
			*d = s != 0
		case bool:
			*d = s
		case nil:
			*d = false
		default:
			return fmt.Errorf("can't read %T as a bool", src)
		}
		return nil
	case **bool:
		if src == nil {
			*d = nil
			return nil
		}
		var b bool
		if err := assign(&b, src); err != nil {
			return err
		}
		*d = &b
		return nil
	case *string:
		switch s := src.(type) {
		case nil:
			*d = ""
		case string:
			*d = s
		case []byte:
			*d = string(s)
		default:
			*d = fmt.Sprint(s)
		}
		return nil
	case **string:
		if src == nil {
			*d = nil
			return nil
		}
		var s string
		if err := assign(&s, src); err != nil {
			return err
		}
		*d = &s
		return nil
	}
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("can't scan into %T", dest)
	}
	target := rv.Elem()
	switch target.Kind() {
	case reflect.Map, reflect.Struct, reflect.Slice:
		if target.Type() == timeType {
			break
		}
		var text []byte
		switch s := src.(type) {
		case nil:
			target.Set(reflect.Zero(target.Type()))
			return nil
		case string:
			text = []byte(s)
		case []byte:
			text = s
		default:
			return fmt.Errorf("can't read %T as JSON into %T", src, dest)
		}
		return json.Unmarshal(text, dest)
	case reflect.Pointer:
		if src == nil {
			target.Set(reflect.Zero(target.Type()))
			return nil
		}
		v := reflect.New(target.Type().Elem())
		if err := assign(v.Interface(), src); err != nil {
			return err
		}
		target.Set(v)
		return nil
	}
	return convert(target, src)
}

// convert reads numbers and text into the kinds that hold them.
func convert(target reflect.Value, src any) error {
	if src == nil {
		target.Set(reflect.Zero(target.Type()))
		return nil
	}
	switch target.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch s := src.(type) {
		case int64:
			target.SetInt(s)
			return nil
		case float64:
			target.SetInt(int64(s))
			return nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if s, ok := src.(int64); ok {
			target.SetUint(uint64(s))
			return nil
		}
	case reflect.Float32, reflect.Float64:
		switch s := src.(type) {
		case float64:
			target.SetFloat(s)
			return nil
		case int64:
			target.SetFloat(float64(s))
			return nil
		}
	case reflect.String:
		switch s := src.(type) {
		case string:
			target.SetString(s)
			return nil
		case []byte:
			target.SetString(string(s))
			return nil
		}
	case reflect.Bool:
		if s, ok := src.(int64); ok {
			target.SetBool(s != 0)
			return nil
		}
	}
	return fmt.Errorf("can't read %T into %s", src, target.Type())
}

func parseTime(src any) (time.Time, error) {
	var s string
	switch v := src.(type) {
	case nil:
		return time.Time{}, nil
	case time.Time:
		return v.UTC(), nil
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return time.Time{}, fmt.Errorf("can't read %T as a time", src)
	}
	for _, layout := range []string{TimeFormat, time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("can't read %q as a time", s)
}
