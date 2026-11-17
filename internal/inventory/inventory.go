// Package inventory holds the resources collected for a diagram, as records
// that depend neither on where they came from nor on which platform they
// belong to. A platform's sources (such as platform/aws/aws_sdk and
// platform/aws/aws_steampipe) produce them, the platform package finishes
// them, and the model reads them.
package inventory

// Region holds the resources found in one region.
type Region struct {
	Name    string
	Records []Record
}
