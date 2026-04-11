package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

type bulkEntry struct {
	EmployeeID  int      `json:"employeeId" binding:"required"`
	ProjectID   int      `json:"projectId" binding:"required"`
	ActivityID  int      `json:"activityId" binding:"required"`
	Hours       string   `json:"hours" binding:"required"`
	Dates       []string `json:"dates" binding:"required"`
	Description string   `json:"description"`
}

type bulkRequest struct {
	Entries []bulkEntry `json:"entries" binding:"required"`
}

type entryResult struct {
	EmployeeID int    `json:"employeeId"`
	Date       string `json:"date"`
	Status     string `json:"status"` // "ok" or "error"
	Error      string `json:"error,omitempty"`
}

// SubmitHours handles POST /api/v1/hours
func SubmitHours(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	var req bulkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// Build all individual hour entries
	type job struct {
		entry      eboekhouden.HourEntry
		employeeID int
		date       string
	}

	var jobs []job
	for _, e := range req.Entries {
		for _, dateStr := range e.Dates {
			date, err := parseDate(dateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid date %q: %v", dateStr, err)})
				return
			}

			jobs = append(jobs, job{
				entry: eboekhouden.HourEntry{
					GebruikerID:  e.EmployeeID,
					AantalUren:   e.Hours,
					Datum:        date,
					ActiviteitID: e.ActivityID,
					ProjectID:    e.ProjectID,
					Omschrijving: e.Description,
				},
				employeeID: e.EmployeeID,
				date:       dateStr,
			})
		}
	}

	// Submit with worker pool (3 concurrent)
	results := make([]entryResult, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)

	for i, j := range jobs {
		wg.Add(1)
		go func(idx int, j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			err := client.SubmitHourEntry(j.entry)
			if err != nil {
				results[idx] = entryResult{
					EmployeeID: j.employeeID,
					Date:       j.date,
					Status:     "error",
					Error:      err.Error(),
				}
			} else {
				results[idx] = entryResult{
					EmployeeID: j.employeeID,
					Date:       j.date,
					Status:     "ok",
				}
			}
		}(i, j)
	}

	wg.Wait()

	c.JSON(http.StatusOK, gin.H{"results": results})
}

// parseDate converts "YYYY-MM-DD" to eboekhouden.Date with integer day, month, and year fields.
func parseDate(s string) (eboekhouden.Date, error) {
	parts := strings.Split(s, "-")
	if len(parts) != 3 {
		return eboekhouden.Date{}, fmt.Errorf("expected YYYY-MM-DD format")
	}

	year, err := strconv.Atoi(parts[0])
	if err != nil {
		return eboekhouden.Date{}, err
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil {
		return eboekhouden.Date{}, err
	}
	day, err := strconv.Atoi(parts[2])
	if err != nil {
		return eboekhouden.Date{}, err
	}

	return eboekhouden.Date{Day: day, Month: month, Year: year}, nil
}
