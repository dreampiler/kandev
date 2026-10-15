package models

// AdmittedExecution is the durable half of shared execution capacity.
type AdmittedExecution struct {
	ID    string `db:"id"`
	Grade string `db:"admitted_execution_grade"`
}
