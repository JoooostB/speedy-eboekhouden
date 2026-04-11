package database

import (
	"context"
	"fmt"
	"time"
)

// Team represents a team (maps 1:1 to an e-boekhouden administratie).
type Team struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OwnerID   string    `json:"ownerId"`
	CreatedAt time.Time `json:"createdAt"`
}

// TeamMember represents a user's membership in a team.
type TeamMember struct {
	TeamID string `json:"teamId"`
	UserID string `json:"userId"`
	Role   string `json:"role"`
	Name   string `json:"name"`
	Email  string `json:"email"`
}

// CreateTeam creates a new team and adds the owner as a member with "owner" role.
func (db *DB) CreateTeam(ctx context.Context, name, ownerID string) (*Team, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	t := &Team{}
	err = tx.QueryRow(ctx,
		`INSERT INTO teams (name, owner_id) VALUES ($1, $2)
		 RETURNING id, name, owner_id, created_at`,
		name, ownerID,
	).Scan(&t.ID, &t.Name, &t.OwnerID, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("creating team: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO team_members (team_id, user_id, role) VALUES ($1, $2, 'owner')`,
		t.ID, ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("adding owner as member: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}

	return t, nil
}

// GetTeamsByUserID returns all teams a user belongs to.
func (db *DB) GetTeamsByUserID(ctx context.Context, userID string) ([]*Team, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT t.id, t.name, t.owner_id, t.created_at
		 FROM teams t
		 JOIN team_members tm ON t.id = tm.team_id
		 WHERE tm.user_id = $1
		 ORDER BY t.created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying teams: %w", err)
	}
	defer rows.Close()

	var teams []*Team
	for rows.Next() {
		t := &Team{}
		if err := rows.Scan(&t.ID, &t.Name, &t.OwnerID, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning team: %w", err)
		}
		teams = append(teams, t)
	}
	return teams, rows.Err()
}

// GetTeamByID returns a single team.
func (db *DB) GetTeamByID(ctx context.Context, teamID string) (*Team, error) {
	t := &Team{}
	err := db.Pool.QueryRow(ctx,
		`SELECT id, name, owner_id, created_at FROM teams WHERE id = $1`,
		teamID,
	).Scan(&t.ID, &t.Name, &t.OwnerID, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting team: %w", err)
	}
	return t, nil
}

// GetMembers returns all members of a team.
func (db *DB) GetMembers(ctx context.Context, teamID string) ([]*TeamMember, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT tm.team_id, tm.user_id, tm.role, u.name, u.email
		 FROM team_members tm
		 JOIN users u ON tm.user_id = u.id
		 WHERE tm.team_id = $1
		 ORDER BY tm.role, u.name`,
		teamID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying members: %w", err)
	}
	defer rows.Close()

	var members []*TeamMember
	for rows.Next() {
		m := &TeamMember{}
		if err := rows.Scan(&m.TeamID, &m.UserID, &m.Role, &m.Name, &m.Email); err != nil {
			return nil, fmt.Errorf("scanning member: %w", err)
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// AddMember adds a user to a team.
func (db *DB) AddMember(ctx context.Context, teamID, userID, role string) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO team_members (team_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (team_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		teamID, userID, role,
	)
	if err != nil {
		return fmt.Errorf("adding member: %w", err)
	}
	return nil
}

// RemoveMember removes a user from a team.
func (db *DB) RemoveMember(ctx context.Context, teamID, userID string) error {
	_, err := db.Pool.Exec(ctx,
		`DELETE FROM team_members WHERE team_id = $1 AND user_id = $2`,
		teamID, userID,
	)
	if err != nil {
		return fmt.Errorf("removing member: %w", err)
	}
	return nil
}

// IsMember checks if a user belongs to a team.
func (db *DB) IsMember(ctx context.Context, teamID, userID string) (bool, error) {
	var exists bool
	err := db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM team_members WHERE team_id = $1 AND user_id = $2)`,
		teamID, userID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking membership: %w", err)
	}
	return exists, nil
}
