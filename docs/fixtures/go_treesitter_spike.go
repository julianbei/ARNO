package fixtures

import "fmt"

type (
	Alpha struct {
		ID string
	}
	Beta interface {
		Do()
	}
)

func BuildAlpha(
	id string,
) *Alpha {
	return &Alpha{ID: id}
}

func (a *Alpha) Print() {
	fmt.Println(a.ID)
}
