package data

import (
	"fmt"
	"io"

	"github.com/elgs/gojq"
)

// Getter is the interface used by Points to get next points.
type Getter interface {
	io.Closer
	Get() (*gojq.JQ, error)
}

type Point struct {
	ID        string  `json:"id"`
	Value     float64 `json:"value"`
	IsCounter bool    `json:"isCounter"`
}

// Points is a series of Size data points gathered from Source.
type Points struct {
	Source Getter
	Target chan<- Point
}

// Run get data from the source and capture metrics following specs.
func (p *Points) Run(specs []Spec) error {
	for {
		jq, err := p.Source.Get()
		if err != nil {
			return fmt.Errorf("input error: %v", err)
		}
		if jq == nil {
			break
		}
		for _, spec := range specs {
			for _, f := range spec.Fields {
				v, err := jq.Query(f.Name)
				if err != nil {
					return fmt.Errorf("cannot get %s: %v", f.Name, err)
				}
				n, ok := v.(float64)
				if !ok {
					return fmt.Errorf("invalid type %s: %T", f.Name, v)
				}
				p.Target <- Point{
					ID:        f.ID,
					Value:     n,
					IsCounter: f.IsCounter,
				}
			}
		}
	}
	return nil
}

// Close calls Close on Source.
func (p *Points) Close() error {
	return p.Source.Close()
}
