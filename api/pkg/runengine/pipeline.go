package runengine

import (
	"github.com/helios-cicd/helios/api/pkg/dsl"
	"github.com/helios-cicd/helios/api/pkg/engine"
)

func parsePipeline(raw []byte) (*dsl.Pipeline, error) {
	res, err := dsl.Parse(raw)
	if err != nil {
		return nil, err
	}
	return res.Pipeline, nil
}

func buildDAG(p *dsl.Pipeline) (*engine.DAG, error) {
	expanded, err := engine.ExpandPipeline(p)
	if err != nil {
		return nil, err
	}
	dag := engine.BuildDAG(expanded)
	if cycles := dag.DetectCycles(); len(cycles) > 0 {
		return nil, cycles[0]
	}
	return dag, nil
}
