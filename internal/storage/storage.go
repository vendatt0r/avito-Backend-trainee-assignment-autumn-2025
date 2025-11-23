package storage

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"Backend-trainee-assignment-autumn-2025/internal/models"
)

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

var (
	ErrTeamNotFound = errors.New("team not found")
	ErrUserNotFound = errors.New("user not found")
	ErrPRExists     = errors.New("PR_EXISTS")
	ErrPRNotFound   = errors.New("pr not found")
	ErrPRMerged     = errors.New("PR_MERGED")
	ErrNotAssigned  = errors.New("NOT_ASSIGNED")
	ErrNoCandidate  = errors.New("NO_CANDIDATE")
)

func (s *Store) UpsertTeam(ctx context.Context, t models.Team) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	_, err = tx.Exec(ctx,
		`INSERT INTO teams(team_name) VALUES($1)
		 ON CONFLICT (team_name) DO NOTHING`,
		t.TeamName,
	)
	if err != nil {
		return err
	}

	for _, m := range t.Members {
		_, err := tx.Exec(ctx,
			`INSERT INTO users(user_id, username, team_name, is_active)
             VALUES($1,$2,$3,$4)
			 ON CONFLICT (user_id)
			 DO UPDATE SET username=EXCLUDED.username,
			               team_name=EXCLUDED.team_name,
			               is_active=EXCLUDED.is_active`,
			m.UserID, m.Username, t.TeamName, m.IsActive,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) GetTeam(ctx context.Context, teamName string) (models.Team, error) {
	var t models.Team

	rows, err := s.db.Query(ctx,
		`SELECT user_id, username, is_active
         FROM users
         WHERE team_name=$1`,
		teamName,
	)
	if err != nil {
		return t, err
	}
	defer rows.Close()

	members := []models.TeamMember{}
	for rows.Next() {
		var m models.TeamMember
		if err := rows.Scan(&m.UserID, &m.Username, &m.IsActive); err != nil {
			return t, err
		}
		members = append(members, m)
	}

	if len(members) == 0 {
		var exists bool
		err = s.db.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM teams WHERE team_name=$1)`,
			teamName,
		).Scan(&exists)
		if err != nil {
			return t, err
		}
		if !exists {
			return t, ErrTeamNotFound
		}
	}

	t.TeamName = teamName
	t.Members = members
	return t, nil
}

func (s *Store) SetUserActive(ctx context.Context, userID string, isActive bool) (models.User, error) {
	var u models.User

	cmd, err := s.db.Exec(ctx,
		`UPDATE users SET is_active=$1 WHERE user_id=$2`,
		isActive, userID,
	)
	if err != nil {
		return u, err
	}
	if cmd.RowsAffected() == 0 {
		return u, ErrUserNotFound
	}

	err = s.db.QueryRow(ctx,
		`SELECT user_id, username, team_name, is_active
         FROM users WHERE user_id=$1`,
		userID,
	).Scan(&u.UserID, &u.Username, &u.TeamName, &u.IsActive)

	return u, err
}

func (s *Store) GetUser(ctx context.Context, userID string) (models.User, error) {
	var u models.User
	err := s.db.QueryRow(ctx,
		`SELECT user_id, username, team_name, is_active
         FROM users WHERE user_id=$1`,
		userID,
	).Scan(&u.UserID, &u.Username, &u.TeamName, &u.IsActive)

	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrUserNotFound
	}
	return u, err
}

func (s *Store) CreatePR(ctx context.Context, pr models.PullRequest) (models.PullRequest, error) {
	var exists bool
	err := s.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pull_requests WHERE pull_request_id=$1)`,
		pr.PullRequestID,
	).Scan(&exists)
	if err != nil {
		return pr, err
	}
	if exists {
		return pr, ErrPRExists
	}

	var team string
	err = s.db.QueryRow(ctx,
		`SELECT team_name FROM users WHERE user_id=$1`,
		pr.AuthorID,
	).Scan(&team)
	if errors.Is(err, pgx.ErrNoRows) {
		return pr, ErrUserNotFound
	}
	if err != nil {
		return pr, err
	}

	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return pr, err
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return pr, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	_, err = tx.Exec(ctx,
		`INSERT INTO pull_requests(pull_request_id, pull_request_name, author_id, status)
		 VALUES ($1, $2, $3, 'OPEN')`,
		pr.PullRequestID, pr.PullRequestName, pr.AuthorID,
	)
	if err != nil {
		return pr, err
	}

	rows, err := tx.Query(ctx,
		`SELECT user_id FROM users
         WHERE team_name=$1
           AND is_active=true
           AND user_id <> $2
         ORDER BY random()
         LIMIT 2`,
		team, pr.AuthorID,
	)
	if err != nil {
		return pr, err
	}

	var reviewers []string

	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return pr, err
		}
		reviewers = append(reviewers, uid)
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		return pr, err
	}

	for _, uid := range reviewers {
		_, err = tx.Exec(ctx,
			`INSERT INTO pr_reviewers(pull_request_id, user_id)
			 VALUES($1,$2)`,
			pr.PullRequestID, uid,
		)
		if err != nil {
			return pr, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return pr, err
	}

	return s.GetPR(ctx, pr.PullRequestID)
}

func (s *Store) GetPR(ctx context.Context, prID string) (models.PullRequest, error) {
	var p models.PullRequest
	var createdAt time.Time
	var mergedAt *time.Time

	err := s.db.QueryRow(ctx,
		`SELECT pull_request_id, pull_request_name, author_id, status, created_at, merged_at
         FROM pull_requests WHERE pull_request_id=$1`,
		prID,
	).Scan(&p.PullRequestID, &p.PullRequestName, &p.AuthorID, &p.Status, &createdAt, &mergedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrPRNotFound
	}
	if err != nil {
		return p, err
	}

	p.CreatedAt = &createdAt
	if mergedAt != nil {
		p.MergedAt = mergedAt
	}

	rows, err := s.db.Query(ctx,
		`SELECT user_id FROM pr_reviewers WHERE pull_request_id=$1`,
		prID,
	)
	if err != nil {
		return p, err
	}
	defer rows.Close()

	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return p, err
		}
		p.AssignedReviewers = append(p.AssignedReviewers, uid)
	}

	return p, nil
}

func (s *Store) MergePR(ctx context.Context, prID string) (models.PullRequest, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.PullRequest{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	var status string
	var mergedAt *time.Time

	err = tx.QueryRow(ctx,
		`SELECT status, merged_at
         FROM pull_requests
         WHERE pull_request_id=$1 FOR UPDATE`,
		prID,
	).Scan(&status, &mergedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return models.PullRequest{}, ErrPRNotFound
	}
	if err != nil {
		return models.PullRequest{}, err
	}

	if status == "MERGED" {
		_ = tx.Commit(ctx)
		return s.GetPR(ctx, prID)
	}

	now := time.Now().UTC()

	_, err = tx.Exec(ctx,
		`UPDATE pull_requests
         SET status='MERGED', merged_at=$1
         WHERE pull_request_id=$2`,
		now, prID,
	)
	if err != nil {
		return models.PullRequest{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.PullRequest{}, err
	}

	return s.GetPR(ctx, prID)
}

func (s *Store) ReassignReviewer(ctx context.Context, prID, oldUserID string) (models.PullRequest, string, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.PullRequest{}, "", err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var status string
	err = tx.QueryRow(ctx,
		`SELECT status FROM pull_requests
         WHERE pull_request_id=$1 FOR UPDATE`,
		prID,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.PullRequest{}, "", ErrPRNotFound
	}
	if err != nil {
		return models.PullRequest{}, "", err
	}
	if status == "MERGED" {
		return models.PullRequest{}, "", ErrPRMerged
	}

	var assigned bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS(
           SELECT 1 FROM pr_reviewers
           WHERE pull_request_id=$1 AND user_id=$2
         )`,
		prID, oldUserID,
	).Scan(&assigned)
	if err != nil {
		return models.PullRequest{}, "", err
	}
	if !assigned {
		return models.PullRequest{}, "", ErrNotAssigned
	}

	var team string
	err = tx.QueryRow(ctx,
		`SELECT team_name FROM users WHERE user_id=$1`,
		oldUserID,
	).Scan(&team)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.PullRequest{}, "", ErrUserNotFound
	}
	if err != nil {
		return models.PullRequest{}, "", err
	}

	var authorID string
	err = tx.QueryRow(ctx,
		`SELECT author_id FROM pull_requests WHERE pull_request_id=$1`,
		prID,
	).Scan(&authorID)
	if err != nil {
		return models.PullRequest{}, "", err
	}

	rows, err := tx.Query(ctx,
		`SELECT user_id FROM pr_reviewers WHERE pull_request_id=$1`,
		prID,
	)
	if err != nil {
		return models.PullRequest{}, "", err
	}

	taken := map[string]bool{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return models.PullRequest{}, "", err
		}
		if u != oldUserID {
			taken[u] = true
		}
	}
	rows.Close()

	cRows, err := tx.Query(ctx,
		`SELECT user_id FROM users
         WHERE team_name=$1
           AND is_active=true
           AND user_id <> $2
         ORDER BY random()`,
		team, authorID,
	)
	if err != nil {
		return models.PullRequest{}, "", err
	}
	defer cRows.Close()

	candidates := []string{}
	for cRows.Next() {
		var uid string
		if err := cRows.Scan(&uid); err != nil {
			return models.PullRequest{}, "", err
		}
		if uid != oldUserID && !taken[uid] {
			candidates = append(candidates, uid)
		}
	}
	if len(candidates) == 0 {
		return models.PullRequest{}, "", ErrNoCandidate
	}

	newReviewer := candidates[rand.Intn(len(candidates))]

	_, err = tx.Exec(ctx,
		`DELETE FROM pr_reviewers
		 WHERE pull_request_id=$1 AND user_id=$2`,
		prID, oldUserID,
	)
	if err != nil {
		return models.PullRequest{}, "", err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO pr_reviewers(pull_request_id, user_id)
         VALUES($1,$2)`,
		prID, newReviewer,
	)
	if err != nil {
		return models.PullRequest{}, "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.PullRequest{}, "", err
	}

	pr, err := s.GetPR(ctx, prID)
	return pr, newReviewer, err
}

func (s *Store) GetPRsForReviewer(ctx context.Context, userID string) ([]models.PullRequestShort, error) {
	rows, err := s.db.Query(ctx,
		`SELECT p.pull_request_id, p.pull_request_name, p.author_id, p.status
         FROM pull_requests p
         JOIN pr_reviewers r ON p.pull_request_id = r.pull_request_id
         WHERE r.user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []models.PullRequestShort
	for rows.Next() {
		var p models.PullRequestShort
		if err := rows.Scan(&p.PullRequestID, &p.PullRequestName, &p.AuthorID, &p.Status); err != nil {
			return nil, err
		}
		res = append(res, p)
	}
	return res, nil
}

func (s *Store) GetReviewerStats(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.Query(ctx,
		`SELECT user_id, COUNT(*)
         FROM pr_reviewers
         GROUP BY user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := map[string]int{}
	for rows.Next() {
		var uid string
		var cnt int
		if err := rows.Scan(&uid, &cnt); err != nil {
			return nil, err
		}
		stats[uid] = cnt
	}
	return stats, nil
}

func (s *Store) GetPRStats(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.Query(ctx,
		`SELECT pull_request_id, COUNT(*)
         FROM pr_reviewers
         GROUP BY pull_request_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := map[string]int{}
	for rows.Next() {
		var prID string
		var cnt int
		if err := rows.Scan(&prID, &cnt); err != nil {
			return nil, err
		}
		stats[prID] = cnt
	}
	return stats, nil
}

func (s *Store) BulkDeactivateUsers(ctx context.Context, teamName string, userIDs []string) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	_, err = tx.Exec(ctx,
		`UPDATE users
         SET is_active = false
         WHERE team_name=$1 AND user_id = ANY($2)`,
		teamName, userIDs,
	)
	if err != nil {
		return err
	}

	activeRows, err := tx.Query(ctx,
		`SELECT user_id FROM users
         WHERE team_name=$1 AND is_active=true`,
		teamName,
	)
	if err != nil {
		return err
	}

	active := []string{}
	activeSet := map[string]bool{}

	for activeRows.Next() {
		var uid string
		if err := activeRows.Scan(&uid); err != nil {
			return err
		}
		active = append(active, uid)
		activeSet[uid] = true
	}
	activeRows.Close()

	prRows, err := tx.Query(ctx,
		`SELECT pr.pull_request_id, pr.author_id,
                COALESCE(array_agg(r.user_id), '{}') AS reviewers
         FROM pull_requests pr
         LEFT JOIN pr_reviewers r
            ON pr.pull_request_id = r.pull_request_id
         WHERE pr.status='OPEN'
         GROUP BY pr.pull_request_id`,
	)
	if err != nil {
		return err
	}
	defer prRows.Close()

	type PR struct {
		ID        string
		Author    string
		Reviewers []string
	}

	var prs []PR
	for prRows.Next() {
		var id, author string
		var reviewers []string
		if err := prRows.Scan(&id, &author, &reviewers); err != nil {
			return err
		}

		prs = append(prs, PR{id, author, reviewers})
	}

	for _, pr := range prs {
		taken := map[string]bool{}
		final := []string{}

		for _, r := range pr.Reviewers {
			taken[r] = true
			if activeSet[r] {
				final = append(final, r)
			}
		}

		if len(final) == 0 {
			candidates := []string{}
			for _, u := range active {
				if u != pr.Author && !taken[u] {
					candidates = append(candidates, u)
				}
			}

			if len(candidates) > 0 {
				final = append(final, candidates[rand.Intn(len(candidates))])
			}
		}

		_, err := tx.Exec(ctx,
			`DELETE FROM pr_reviewers WHERE pull_request_id=$1`,
			pr.ID,
		)
		if err != nil {
			return err
		}

		for _, r := range final {
			_, err := tx.Exec(ctx,
				`INSERT INTO pr_reviewers(pull_request_id, user_id)
                 VALUES($1,$2)`,
				pr.ID, r,
			)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}
