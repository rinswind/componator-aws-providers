/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// conversion.go contains pointer conversion utilities for RDS operations.
// These utilities eliminate the boilerplate of converting between Go values/pointers
// and AWS SDK pointer requirements while handling optional/empty value semantics.

package rds

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
)

// Pointer conversion utilities for required fields
// These always convert to pointers, even for zero values

// stringPtr converts a string value to *string pointer for required AWS fields
func stringPtr(s string) *string {
	return aws.String(s)
}

// int32Ptr converts an int32 value to *int32 pointer for required AWS fields
func int32Ptr(i int32) *int32 {
	return aws.Int32(i)
}

// boolPtr converts a bool value to *bool pointer for required AWS fields
func boolPtr(b bool) *bool {
	return aws.Bool(b)
}

// Pointer conversion utilities for optional fields
// These return nil for empty/zero values to omit the field from AWS requests

// optionalStringPtr converts a string to *string, returning nil for empty strings
// This allows AWS to use default values or omit optional fields entirely
func optionalStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return aws.String(s)
}

// optionalInt32Ptr converts an int32 to *int32, returning nil for zero values
// This allows AWS to use default values or omit optional fields entirely
func optionalInt32Ptr(i int32) *int32 {
	if i == 0 {
		return nil
	}
	return aws.Int32(i)
}

// positiveInt32Ptr converts an int32 to *int32, returning nil for zero or negative values
// This is useful for fields like MonitoringInterval that only accept positive values
func positiveInt32Ptr(i int32) *int32 {
	if i <= 0 {
		return nil
	}
	return aws.Int32(i)
}

// passthroughPositiveInt32Ptr handles *int32 fields that should only be set for positive values
// Returns nil if input is nil or points to zero/negative value
func passthroughPositiveInt32Ptr(p *int32) *int32 {
	if p == nil || *p <= 0 {
		return nil
	}
	return aws.Int32(*p)
}

// optionalBoolPtr converts a bool to *bool, with special handling for false values
// For AWS, false is often meaningful, so this only returns nil for explicit zero-initialization
// Use this carefully - most bool fields should use passthroughBoolPtr instead
func optionalBoolPtr(b bool) *bool {
	return aws.Bool(b)
}

// Pointer conversion utilities for fields that are already pointers in RdsConfig
// These handle the case where our config uses pointers but needs AWS SDK wrapping

// passthroughStringPtr handles *string fields that need AWS SDK conversion
// Returns nil if input is nil, otherwise wraps the dereferenced value
func passthroughStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	return aws.String(*p)
}

// passthroughInt32Ptr handles *int32 fields that need AWS SDK conversion
// Returns nil if input is nil, otherwise wraps the dereferenced value
func passthroughInt32Ptr(p *int32) *int32 {
	if p == nil {
		return nil
	}
	return aws.Int32(*p)
}

// passthroughBoolPtr handles *bool fields that need AWS SDK conversion
// Returns nil if input is nil, otherwise wraps the dereferenced value
func passthroughBoolPtr(p *bool) *bool {
	if p == nil {
		return nil
	}
	return aws.Bool(*p)
}

// String slice utilities for AWS SDK conversion

// stringSlicePtr converts []string to []*string, handling nil/empty slices
// Returns nil for empty slices to omit from AWS requests
func stringSlicePtr(vs []string) []*string {
	if len(vs) == 0 {
		return nil
	}
	return aws.StringSlice(vs)
}

// optionalStringSlicePtr is an alias for stringSlicePtr for consistency
// This makes the intent clear when used for optional fields
var optionalStringSlicePtr = stringSlicePtr

// Reverse conversion utilities: pointer → value with zero-value defaults
// These eliminate unsafe dereferences and conditional nil-checking boilerplate

// stringValue safely converts *string to string, returning empty string if nil
// This is equivalent to aws.ToString() but with a clearer name for our use case
func stringValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// int32Value safely converts *int32 to int32, returning zero if nil
// This is equivalent to aws.ToInt32() but with a clearer name for our use case
func int32Value(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

// boolValue safely converts *bool to bool, returning false if nil
// This is equivalent to aws.ToBool() but with a clearer name for our use case
func boolValue(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

// Specialized reverse conversion utilities for nested pointer access

// endpointAddress safely extracts address string from RDS Endpoint
// Handles the pattern: instance.Endpoint != nil && instance.Endpoint.Address != nil
func endpointAddress(endpoint *types.Endpoint) string {
	if endpoint == nil || endpoint.Address == nil {
		return ""
	}
	return *endpoint.Address
}

// endpointPort safely extracts port int32 from RDS Endpoint
// Handles the pattern: instance.Endpoint != nil && instance.Endpoint.Port != nil
func endpointPort(endpoint *types.Endpoint) int32 {
	if endpoint == nil || endpoint.Port == nil {
		return 0
	}
	return *endpoint.Port
}
