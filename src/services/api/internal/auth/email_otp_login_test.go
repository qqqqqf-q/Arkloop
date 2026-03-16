package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database"

	"github.com/google/uuid"
)

type noopRiskControl struct{}

func (noopRiskControl) AllowSend(context.Context, string) error           { return nil }
func (noopRiskControl) EnsureVerifyAllowed(context.Context, string) error { return nil }
func (noopRiskControl) RecordVerifyFailure(context.Context, string) error { return nil }
func (noopRiskControl) ResetVerifyState(context.Context, string) error    { return nil }

type stubRow struct {
	scanFunc func(dest ...any) error
}

func (r *stubRow) Scan(dest ...any) error { return r.scanFunc(dest...) }

type stubQuerier struct {
	queryRowFn func(ctx context.Context, sql string, args ...any) database.Row
	execFn     func(ctx context.Context, sql string, args ...any) (database.Result, error)
}

func (q *stubQuerier) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	if q.queryRowFn != nil {
		return q.queryRowFn(ctx, sql, args...)
	}
	return &stubRow{scanFunc: func(dest ...any) error { return database.ErrNoRows }}
}

func (q *stubQuerier) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	if q.execFn != nil {
		return q.execFn(ctx, sql, args...)
	}
	return stubResult{tag: "OK 0"}, nil
}

func (q *stubQuerier) Query(_ context.Context, _ string, _ ...any) (database.Rows, error) {
	return nil, errors.New("Query not stubbed")
}

type stubResult struct{ tag string }

func (r stubResult) RowsAffected() int64 {
	// Parse "OK N" format
	parts := strings.Fields(r.tag)
	if len(parts) >= 2 {
		var n int64
		for _, c := range parts[len(parts)-1] {
			if c >= '0' && c <= '9' {
				n = n*10 + int64(c-'0')
			}
		}
		return n
	}
	return 0
}

func (r stubResult) String() string { return r.tag }

type otpTestEnv struct {
	userQ       *stubQuerier
	otpQ        *stubQuerier
	jobQ        *stubQuerier
	refreshQ    *stubQuerier
	membershipQ *stubQuerier
	tokenSvc    *JwtAccessTokenService
	svc         *EmailOTPLoginService
}

func newOTPTestEnv(t *testing.T) *otpTestEnv {
	t.Helper()
	userQ := &stubQuerier{}
	otpQ := &stubQuerier{}
	jobQ := &stubQuerier{}
	refreshQ := &stubQuerier{}
	membershipQ := &stubQuerier{}

	userRepo, _ := data.NewUserRepository(userQ)
	otpRepo, _ := data.NewEmailOTPTokenRepository(otpQ)
	jobRepo, _ := data.NewJobRepository(jobQ)
	refreshRepo, _ := data.NewRefreshTokenRepository(refreshQ)
	membershipRepo, _ := data.NewAccountMembershipRepository(membershipQ)
	tokenSvc, _ := NewJwtAccessTokenService("test-secret-32-bytes-long-enough!", 3600, 86400)

	svc, err := NewEmailOTPLoginService(userRepo, otpRepo, jobRepo, tokenSvc, refreshRepo, membershipRepo, noopRiskControl{}, nil)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	return &otpTestEnv{
		userQ: userQ, otpQ: otpQ, jobQ: jobQ,
		refreshQ: refreshQ, membershipQ: membershipQ,
		tokenSvc: tokenSvc, svc: svc,
	}
}

func scanActiveUser(userID uuid.UUID, username, email string, locale *string, emailVerifiedAt *time.Time) func(dest ...any) error {
	return func(dest ...any) error {
		*dest[0].(*uuid.UUID) = userID
		*dest[1].(*string) = username
		emailPtr := dest[2].(**string)
		*emailPtr = &email
		*dest[3].(**time.Time) = emailVerifiedAt
		*dest[4].(*string) = "active"
		*dest[5].(**time.Time) = nil
		*dest[6].(**string) = nil
		*dest[7].(**string) = locale
		*dest[8].(**string) = nil
		*dest[9].(**time.Time) = nil
		*dest[10].(*time.Time) = time.Time{}
		*dest[11].(*time.Time) = time.Now()
		return nil
	}
}

func TestNewEmailOTPLoginServiceNilDeps(t *testing.T) {
	tokenSvc, _ := NewJwtAccessTokenService("secret-32-chars-long-enough!!", 3600, 86400)
	q := &stubQuerier{}
	userRepo, _ := data.NewUserRepository(q)
	otpRepo, _ := data.NewEmailOTPTokenRepository(q)
	jobRepo, _ := data.NewJobRepository(q)
	refreshRepo, _ := data.NewRefreshTokenRepository(q)
	membershipRepo, _ := data.NewAccountMembershipRepository(q)

	cases := []struct {
		name string
		call func() (*EmailOTPLoginService, error)
	}{
		{"nil_userRepo", func() (*EmailOTPLoginService, error) {
			return NewEmailOTPLoginService(nil, otpRepo, jobRepo, tokenSvc, refreshRepo, membershipRepo, noopRiskControl{}, nil)
		}},
		{"nil_otpRepo", func() (*EmailOTPLoginService, error) {
			return NewEmailOTPLoginService(userRepo, nil, jobRepo, tokenSvc, refreshRepo, membershipRepo, noopRiskControl{}, nil)
		}},
		{"nil_jobRepo", func() (*EmailOTPLoginService, error) {
			return NewEmailOTPLoginService(userRepo, otpRepo, nil, tokenSvc, refreshRepo, membershipRepo, noopRiskControl{}, nil)
		}},
		{"nil_tokenService", func() (*EmailOTPLoginService, error) {
			return NewEmailOTPLoginService(userRepo, otpRepo, jobRepo, nil, refreshRepo, membershipRepo, noopRiskControl{}, nil)
		}},
		{"nil_refreshTokenRepo", func() (*EmailOTPLoginService, error) {
			return NewEmailOTPLoginService(userRepo, otpRepo, jobRepo, tokenSvc, nil, membershipRepo, noopRiskControl{}, nil)
		}},
		{"nil_membershipRepo", func() (*EmailOTPLoginService, error) {
			return NewEmailOTPLoginService(userRepo, otpRepo, jobRepo, tokenSvc, refreshRepo, nil, noopRiskControl{}, nil)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.call()
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestNewEmailOTPLoginServiceAllValid(t *testing.T) {
	env := newOTPTestEnv(t)
	if env.svc == nil {
		t.Fatalf("service must not be nil")
	}
}

func TestSendLoginOTPUserNotFound(t *testing.T) {
	env := newOTPTestEnv(t)
	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(_ ...any) error { return database.ErrNoRows }}
	}

	err := env.svc.SendLoginOTP(context.Background(), "unknown@example.com")
	if err != nil {
		t.Fatalf("expected silent nil for unknown user, got: %v", err)
	}
}

func TestSendLoginOTPUserSuspended(t *testing.T) {
	env := newOTPTestEnv(t)
	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(dest ...any) error {
			*dest[0].(*uuid.UUID) = uuid.New()
			*dest[1].(*string) = "test"
			*dest[2].(**string) = ptrStr("suspended@example.com")
			*dest[3].(**time.Time) = nil
			*dest[4].(*string) = "suspended"
			*dest[5].(**time.Time) = nil
			*dest[6].(**string) = nil
			*dest[7].(**string) = nil
			*dest[8].(**string) = nil
			*dest[9].(**time.Time) = nil
			*dest[10].(*time.Time) = time.Time{}
			*dest[11].(*time.Time) = time.Now()
			return nil
		}}
	}

	err := env.svc.SendLoginOTP(context.Background(), "suspended@example.com")
	if err != nil {
		t.Fatalf("expected silent nil for suspended user, got: %v", err)
	}
}

func TestSendLoginOTPSuccess(t *testing.T) {
	env := newOTPTestEnv(t)
	userID := uuid.New()

	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: scanActiveUser(userID, "alice", "alice@example.com", nil, nil)}
	}

	env.otpQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(dest ...any) error {
			*dest[0].(*uuid.UUID) = uuid.New()
			*dest[1].(*uuid.UUID) = userID
			*dest[2].(*string) = "hash"
			*dest[3].(*time.Time) = time.Now().Add(time.Hour)
			*dest[4].(**time.Time) = nil
			*dest[5].(*time.Time) = time.Now()
			return nil
		}}
	}

	var enqueueCount int
	env.jobQ.execFn = func(_ context.Context, sql string, _ ...any) (database.Result, error) {
		if strings.Contains(sql, "INSERT INTO jobs") {
			enqueueCount++
		}
		return stubResult{tag: "INSERT 0 1"}, nil
	}

	err := env.svc.SendLoginOTP(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enqueueCount == 0 {
		t.Errorf("expected email job to be enqueued")
	}
}

func TestSendLoginOTPChineseLocale(t *testing.T) {
	env := newOTPTestEnv(t)
	userID := uuid.New()
	zhLocale := "zh"

	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: scanActiveUser(userID, "bob", "bob@example.com", &zhLocale, nil)}
	}

	env.otpQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(dest ...any) error {
			*dest[0].(*uuid.UUID) = uuid.New()
			*dest[1].(*uuid.UUID) = userID
			*dest[2].(*string) = "hash"
			*dest[3].(*time.Time) = time.Now().Add(time.Hour)
			*dest[4].(**time.Time) = nil
			*dest[5].(*time.Time) = time.Now()
			return nil
		}}
	}

	var capturedSubject string
	env.jobQ.execFn = func(_ context.Context, sql string, args ...any) (database.Result, error) {
		if strings.Contains(sql, "INSERT INTO jobs") && len(args) >= 3 {
			payload := args[2].(string)
			if strings.Contains(payload, "登录验证码") {
				capturedSubject = "zh"
			}
		}
		return stubResult{tag: "INSERT 0 1"}, nil
	}

	err := env.svc.SendLoginOTP(context.Background(), "bob@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedSubject != "zh" {
		t.Errorf("expected Chinese email subject")
	}
}

func TestSendLoginOTPDbErrorOnLookup(t *testing.T) {
	env := newOTPTestEnv(t)
	dbErr := errors.New("connection refused")
	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(_ ...any) error { return dbErr }}
	}

	err := env.svc.SendLoginOTP(context.Background(), "alice@example.com")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "lookup user") {
		t.Errorf("error should wrap 'lookup user', got: %v", err)
	}
}

func TestSendLoginOTPDbErrorOnOtpCreate(t *testing.T) {
	env := newOTPTestEnv(t)
	userID := uuid.New()

	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: scanActiveUser(userID, "alice", "alice@example.com", nil, nil)}
	}

	env.otpQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(_ ...any) error { return errors.New("disk full") }}
	}

	err := env.svc.SendLoginOTP(context.Background(), "alice@example.com")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "create otp token") {
		t.Errorf("error should wrap 'create otp token', got: %v", err)
	}
}

func TestSendLoginOTPDbErrorOnEnqueue(t *testing.T) {
	env := newOTPTestEnv(t)
	userID := uuid.New()

	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: scanActiveUser(userID, "alice", "alice@example.com", nil, nil)}
	}

	env.otpQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(dest ...any) error {
			*dest[0].(*uuid.UUID) = uuid.New()
			*dest[1].(*uuid.UUID) = userID
			*dest[2].(*string) = "hash"
			*dest[3].(*time.Time) = time.Now().Add(time.Hour)
			*dest[4].(**time.Time) = nil
			*dest[5].(*time.Time) = time.Now()
			return nil
		}}
	}

	env.jobQ.execFn = func(_ context.Context, sql string, _ ...any) (database.Result, error) {
		if strings.Contains(sql, "INSERT INTO jobs") {
			return stubResult{}, errors.New("queue full")
		}
		return stubResult{tag: "OK"}, nil
	}

	err := env.svc.SendLoginOTP(context.Background(), "alice@example.com")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "enqueue") {
		t.Errorf("error should wrap 'enqueue', got: %v", err)
	}
}

func TestVerifyLoginOTPEmptyCode(t *testing.T) {
	env := newOTPTestEnv(t)
	_, err := env.svc.VerifyLoginOTP(context.Background(), "alice@example.com", "")
	if err == nil {
		t.Fatalf("expected error for empty code")
	}
	var otpErr OTPExpiredOrUsedError
	if !errors.As(err, &otpErr) {
		t.Fatalf("expected OTPExpiredOrUsedError, got %T: %v", err, err)
	}
}

func TestVerifyLoginOTPUserNotFound(t *testing.T) {
	env := newOTPTestEnv(t)
	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(_ ...any) error { return database.ErrNoRows }}
	}

	_, err := env.svc.VerifyLoginOTP(context.Background(), "unknown@example.com", "123456")
	if err == nil {
		t.Fatalf("expected error")
	}
	var otpErr OTPExpiredOrUsedError
	if !errors.As(err, &otpErr) {
		t.Fatalf("expected OTPExpiredOrUsedError, got %T: %v", err, err)
	}
}

func TestVerifyLoginOTPUserSuspended(t *testing.T) {
	env := newOTPTestEnv(t)
	userID := uuid.New()

	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(dest ...any) error {
			*dest[0].(*uuid.UUID) = userID
			*dest[1].(*string) = "test"
			*dest[2].(**string) = ptrStr("test@example.com")
			*dest[3].(**time.Time) = nil
			*dest[4].(*string) = "suspended"
			*dest[5].(**time.Time) = nil
			*dest[6].(**string) = nil
			*dest[7].(**string) = nil
			*dest[8].(**string) = nil
			*dest[9].(**time.Time) = nil
			*dest[10].(*time.Time) = time.Time{}
			*dest[11].(*time.Time) = time.Now()
			return nil
		}}
	}

	_, err := env.svc.VerifyLoginOTP(context.Background(), "test@example.com", "123456")
	if err == nil {
		t.Fatalf("expected error")
	}
	var suspErr SuspendedUserError
	if !errors.As(err, &suspErr) {
		t.Fatalf("expected SuspendedUserError, got %T: %v", err, err)
	}
}

func TestVerifyLoginOTPConsumeNotFound(t *testing.T) {
	env := newOTPTestEnv(t)
	userID := uuid.New()

	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: scanActiveUser(userID, "alice", "alice@example.com", nil, nil)}
	}

	env.otpQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(_ ...any) error { return database.ErrNoRows }}
	}

	_, err := env.svc.VerifyLoginOTP(context.Background(), "alice@example.com", "wrong-code")
	if err == nil {
		t.Fatalf("expected error")
	}
	var otpErr OTPExpiredOrUsedError
	if !errors.As(err, &otpErr) {
		t.Fatalf("expected OTPExpiredOrUsedError, got %T: %v", err, err)
	}
}

func TestVerifyLoginOTPConsumeUserMismatch(t *testing.T) {
	env := newOTPTestEnv(t)
	userID := uuid.New()
	otherUserID := uuid.New()

	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: scanActiveUser(userID, "alice", "alice@example.com", nil, nil)}
	}

	env.otpQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(dest ...any) error {
			*dest[0].(*uuid.UUID) = otherUserID
			return nil
		}}
	}

	_, err := env.svc.VerifyLoginOTP(context.Background(), "alice@example.com", "123456")
	if err == nil {
		t.Fatalf("expected error")
	}
	var otpErr OTPExpiredOrUsedError
	if !errors.As(err, &otpErr) {
		t.Fatalf("expected OTPExpiredOrUsedError for user mismatch, got %T: %v", err, err)
	}
}

func TestVerifyLoginOTPConsumeDbError(t *testing.T) {
	env := newOTPTestEnv(t)
	userID := uuid.New()
	dbErr := errors.New("connection reset")

	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: scanActiveUser(userID, "alice", "alice@example.com", nil, nil)}
	}

	env.otpQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(_ ...any) error { return dbErr }}
	}

	_, err := env.svc.VerifyLoginOTP(context.Background(), "alice@example.com", "123456")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("expected wrapped db error, got: %v", err)
	}
}

func TestVerifyLoginOTPSuccessAlreadyVerified(t *testing.T) {
	env := newOTPTestEnv(t)
	userID := uuid.New()
	verifiedAt := time.Now()

	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: scanActiveUser(userID, "alice", "alice@example.com", nil, &verifiedAt)}
	}

	env.otpQ.queryRowFn = func(_ context.Context, sql string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(dest ...any) error {
			*dest[0].(*uuid.UUID) = userID
			return nil
		}}
	}

	env.membershipQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(_ ...any) error { return database.ErrNoRows }}
	}

	env.refreshQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(dest ...any) error {
			*dest[0].(*uuid.UUID) = uuid.New()
			*dest[1].(*uuid.UUID) = userID
			*dest[2].(*string) = "hash"
			*dest[3].(*time.Time) = time.Now().Add(24 * time.Hour)
			*dest[4].(**time.Time) = nil
			*dest[5].(*time.Time) = time.Now()
			*dest[6].(**time.Time) = nil
			return nil
		}}
	}

	pair, err := env.svc.VerifyLoginOTP(context.Background(), "alice@example.com", "123456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AccessToken == "" {
		t.Errorf("access token must not be empty")
	}
	if pair.RefreshToken == "" {
		t.Errorf("refresh token must not be empty")
	}
	if pair.UserID != userID {
		t.Errorf("userID: got %s, want %s", pair.UserID, userID)
	}
}

func TestVerifyLoginOTPSuccessEmailNotVerified(t *testing.T) {
	env := newOTPTestEnv(t)
	userID := uuid.New()

	var setEmailVerifiedCalled bool
	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: scanActiveUser(userID, "alice", "alice@example.com", nil, nil)}
	}
	env.userQ.execFn = func(_ context.Context, sql string, _ ...any) (database.Result, error) {
		if strings.Contains(sql, "email_verified_at") {
			setEmailVerifiedCalled = true
			return stubResult{tag: "UPDATE 1"}, nil
		}
		return stubResult{tag: "OK"}, nil
	}

	env.otpQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(dest ...any) error {
			*dest[0].(*uuid.UUID) = userID
			return nil
		}}
	}

	env.membershipQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(_ ...any) error { return database.ErrNoRows }}
	}

	env.refreshQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(dest ...any) error {
			*dest[0].(*uuid.UUID) = uuid.New()
			*dest[1].(*uuid.UUID) = userID
			*dest[2].(*string) = "hash"
			*dest[3].(*time.Time) = time.Now().Add(24 * time.Hour)
			*dest[4].(**time.Time) = nil
			*dest[5].(*time.Time) = time.Now()
			*dest[6].(**time.Time) = nil
			return nil
		}}
	}

	pair, err := env.svc.VerifyLoginOTP(context.Background(), "alice@example.com", "123456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !setEmailVerifiedCalled {
		t.Errorf("SetEmailVerified should have been called for unverified email")
	}
	if pair.AccessToken == "" {
		t.Errorf("access token must not be empty")
	}
}

func TestVerifyLoginOTPSetEmailVerifiedError(t *testing.T) {
	env := newOTPTestEnv(t)
	userID := uuid.New()

	env.userQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: scanActiveUser(userID, "alice", "alice@example.com", nil, nil)}
	}
	env.userQ.execFn = func(_ context.Context, sql string, _ ...any) (database.Result, error) {
		if strings.Contains(sql, "email_verified_at") {
			return stubResult{}, errors.New("db write failed")
		}
		return stubResult{tag: "OK"}, nil
	}

	env.otpQ.queryRowFn = func(_ context.Context, _ string, _ ...any) database.Row {
		return &stubRow{scanFunc: func(dest ...any) error {
			*dest[0].(*uuid.UUID) = userID
			return nil
		}}
	}

	_, err := env.svc.VerifyLoginOTP(context.Background(), "alice@example.com", "123456")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "set email verified") {
		t.Errorf("error should wrap 'set email verified', got: %v", err)
	}
}

func TestOTPExpiredOrUsedErrorMessage(t *testing.T) {
	err := OTPExpiredOrUsedError{}
	if err.Error() != "otp invalid or expired" {
		t.Errorf("got %q, want %q", err.Error(), "otp invalid or expired")
	}
}

func ptrStr(s string) *string {
	return &s
}
