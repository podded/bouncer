package bouncer

import "fmt"

type Version struct {
	Major int  `json:"major"`
	Minor int  `json:"minor"`
	Patch int  `json:"patch"`
	Dev   bool `json:"dev"`
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d.%s", v.Major, v.Minor, v.Patch, v.Dev)
}

var BuiltVersion = Version{
	Major: 0,
	Minor: 0,
	Patch: 0,
	Dev:   true,
}
