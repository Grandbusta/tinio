package db

import (
	"cmp"
	"database/sql/driver"
	"fmt"
	"time"

	"modernc.org/sqlite"
)

// The functions SQL here may call besides SQLite's own, written the way the
// platform's Postgres ones are, on times kept as TimeFormat text:
//   - now(): this moment.
//   - at_plus(at, seconds): at moved by seconds (NULL if at is).
//   - greatest(a, b, ...) and least(...): the largest and smallest of those
//     not NULL, as in Postgres; SQLite's own max and min give NULL if any is.
func init() {
	sqlite.MustRegisterScalarFunction("now", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
		return Time(time.Now()), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("at_plus", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		if args[0] == nil || args[1] == nil {
			return nil, nil
		}
		at, err := parseTime(args[0])
		if err != nil {
			return nil, err
		}
		var secs float64
		switch n := args[1].(type) {
		case int64:
			secs = float64(n)
		case float64:
			secs = n
		default:
			return nil, fmt.Errorf("at_plus: seconds can't be a %T", args[1])
		}
		return Time(at.Add(time.Duration(secs * float64(time.Second)))), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("greatest", -1, extreme(1))
	sqlite.MustRegisterDeterministicScalarFunction("least", -1, extreme(-1))
}

// extreme picks, of the arguments not NULL, the one furthest in sign's
// direction; NULL if all are.
func extreme(sign int) func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
	return func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		var best driver.Value
		for _, a := range args {
			if a == nil {
				continue
			}
			if best == nil || compare(a, best)*sign > 0 {
				best = a
			}
		}
		return best, nil
	}
}

func compare(a, b driver.Value) int {
	switch x := a.(type) {
	case int64:
		switch y := b.(type) {
		case int64:
			return cmp.Compare(x, y)
		case float64:
			return cmp.Compare(float64(x), y)
		}
	case float64:
		switch y := b.(type) {
		case int64:
			return cmp.Compare(x, float64(y))
		case float64:
			return cmp.Compare(x, y)
		}
	}
	return cmp.Compare(fmt.Sprint(a), fmt.Sprint(b))
}
