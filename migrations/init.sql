CREATE TABLE IF NOT EXISTS teams (
                                     team_name TEXT PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS users (
                                     user_id TEXT PRIMARY KEY,
                                     username TEXT NOT NULL,
                                     team_name TEXT NOT NULL REFERENCES teams(team_name) ON DELETE CASCADE,
    is_active BOOLEAN NOT NULL DEFAULT true
    );


CREATE TABLE IF NOT EXISTS pull_requests (
                                             pull_request_id TEXT PRIMARY KEY,
                                             pull_request_name TEXT NOT NULL,
                                             author_id TEXT NOT NULL REFERENCES users(user_id),
    status TEXT NOT NULL DEFAULT 'OPEN',
    created_at TIMESTAMPTZ DEFAULT now(),
    merged_at TIMESTAMPTZ NULL
    );

CREATE TABLE IF NOT EXISTS pr_reviewers (
                                            pull_request_id TEXT REFERENCES pull_requests(pull_request_id) ON DELETE CASCADE,
    user_id TEXT REFERENCES users(user_id),
    PRIMARY KEY (pull_request_id, user_id)
    );

CREATE INDEX IF NOT EXISTS idx_pr_reviewers_user ON pr_reviewers(user_id);
CREATE INDEX IF NOT EXISTS idx_users_team_active ON users(team_name, is_active);
