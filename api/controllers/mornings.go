package controllers

import (
	"slices"
	"strings"
	"time"

	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
)

// morningView is an agent's morning as their owner sets it: on or off, the
// time (HH:MM, their time zone) and the days (Sunday 0 to Saturday 6). The
// note comes in the chat; there's no email copy on this computer.
type morningView struct {
	On   bool   `json:"on"`
	At   string `json:"at"`
	Days []int  `json:"days"`
}

// defaultMorning is a new agent's: weekdays at 07:30.
var defaultMorning = morningView{On: true, At: "07:30", Days: []int{1, 2, 3, 4, 5}}

func morningOf(ag models.Agent) morningView {
	v := morningView{On: ag.MorningOn, At: ag.MorningAt, Days: []int{}}
	for _, c := range ag.MorningDays {
		v.Days = append(v.Days, int(c-'0'))
	}
	return v
}

// check is v made ready to keep (its days as digits, in order); an error the
// owner can act on if it isn't one.
func (v morningView) check() (days string, err error) {
	if _, err := time.Parse("15:04", v.At); err != nil || len(v.At) != 5 {
		return "", utils.Invalid("morning", "A morning time is like 07:30.")
	}
	ds := slices.Clone(v.Days)
	slices.Sort(ds)
	ds = slices.Compact(ds)
	var b strings.Builder
	for _, d := range ds {
		if d < 0 || d > 6 {
			return "", utils.Invalid("morning", "Days are Sunday (0) to Saturday (6).")
		}
		b.WriteByte(byte('0' + d))
	}
	if v.On && b.Len() == 0 {
		return "", utils.Invalid("morning", "Pick at least one day, or turn mornings off.")
	}
	return b.String(), nil
}
