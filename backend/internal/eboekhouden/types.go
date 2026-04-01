package eboekhouden

// Date represents the date format used by e-boekhouden API.
type Date struct {
	Day   int `json:"day"`
	Month int `json:"month"`
	Year  int `json:"year"`
}

// HourEntry is the payload for submitting a single hour entry.
type HourEntry struct {
	GebruikerID int    `json:"gebruikerId"`
	AantalUren  string `json:"aantalUren"`
	Datum       Date   `json:"datum"`
	ActiviteitID int   `json:"activiteitId"`
	ProjectID   int    `json:"projectId"`
	Omschrijving string `json:"omschrijving,omitempty"`
}

// Employee represents a user from the selectlist endpoint.
type Employee struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Project represents a project from the project/rel endpoint.
type Project struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	CompanyName string `json:"companyName,omitempty"`
}

// Activity represents an activity from the activiteit endpoint.
type Activity struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
