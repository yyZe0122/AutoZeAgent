package corequery

import (
	"errors"
	"fmt"
)

const MaxPageSize = 500

type SortDirection string

const (
	SortAscending  SortDirection = "asc"
	SortDescending SortDirection = "desc"
)

type Page struct {
	Limit  int
	Offset int
}

type TaskListOptions struct {
	Page  Page
	Sort  SortDirection
	State string
}

type PlanListOptions struct {
	Page  Page
	Sort  SortDirection
	State string
}

type ApprovalListOptions struct {
	Page     Page
	Sort     SortDirection
	Decision string
}

type RunListOptions struct {
	Page  Page
	Sort  SortDirection
	State string
}

type SessionListOptions struct {
	Page Page
	Sort SortDirection
}

type TranscriptOptions struct {
	Page Page
}

// MemoryListOptions filters memory_entries for read APIs (ADR-044).
type MemoryListOptions struct {
	Page            Page
	SessionID       string
	Query           string
	Kind            string
	IncludeGlobal   bool
	IncludeArchived bool
}

func validateListOptions(page Page, sort SortDirection) error {
	if page.Limit <= 0 || page.Limit > MaxPageSize {
		return fmt.Errorf("core query limit must be between 1 and %d", MaxPageSize)
	}
	if page.Offset < 0 {
		return errors.New("core query offset must not be negative")
	}
	if sort != SortAscending && sort != SortDescending {
		return errors.New("core query sort direction must be asc or desc")
	}
	return nil
}

func sqlSortDirection(sort SortDirection) string {
	if sort == SortAscending {
		return "ASC"
	}
	return "DESC"
}
