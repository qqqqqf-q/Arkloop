//go:build !desktop

package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

type stubBootstrapCredentialRepo struct {
	cred  *data.UserCredential
	err   error
	calls int
}

func (s *stubBootstrapCredentialRepo) GetByUserID(ctx context.Context, userID uuid.UUID) (*data.UserCredential, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if s.cred == nil || s.cred.UserID != userID {
		return nil, nil
	}
	return s.cred, nil
}

type stubBootstrapMembershipRepo struct {
	err        error
	calls      int
	lastUserID uuid.UUID
	lastRole   string
}

func (s *stubBootstrapMembershipRepo) SetRoleForUser(ctx context.Context, userID uuid.UUID, role string) error {
	s.calls++
	s.lastUserID = userID
	s.lastRole = role
	return s.err
}

type stubBootstrapSettingsRepo struct {
	values   map[string]*data.PlatformSetting
	getErr   error
	setErr   error
	setCalls int
}

func (s *stubBootstrapSettingsRepo) Get(ctx context.Context, key string) (*data.PlatformSetting, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.values == nil {
		return nil, nil
	}
	setting, ok := s.values[key]
	if !ok {
		return nil, nil
	}
	cp := *setting
	return &cp, nil
}

func (s *stubBootstrapSettingsRepo) Set(ctx context.Context, key, value string) (*data.PlatformSetting, error) {
	s.setCalls++
	if s.setErr != nil {
		return nil, s.setErr
	}
	if s.values == nil {
		s.values = make(map[string]*data.PlatformSetting)
	}
	setting := &data.PlatformSetting{Key: key, Value: value}
	s.values[key] = setting
	return setting, nil
}

func TestBootstrapPlatformAdminOnce(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	t.Run("首次执行成功并写入标记", func(t *testing.T) {
		userID := uuid.New()
		credRepo := &stubBootstrapCredentialRepo{
			cred: &data.UserCredential{UserID: userID, Login: "alice"},
		}
		membershipRepo := &stubBootstrapMembershipRepo{}
		settingsRepo := &stubBootstrapSettingsRepo{}

		err := bootstrapPlatformAdminOnce(context.Background(), credRepo, membershipRepo, settingsRepo, userID, logger)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if membershipRepo.calls != 1 {
			t.Fatalf("expected membership updates=1, got %d", membershipRepo.calls)
		}
		if membershipRepo.lastUserID != userID {
			t.Fatalf("expected user_id=%s, got %s", userID, membershipRepo.lastUserID)
		}
		if membershipRepo.lastRole != auth.RolePlatformAdmin {
			t.Fatalf("expected role=%q, got %q", auth.RolePlatformAdmin, membershipRepo.lastRole)
		}
		if settingsRepo.setCalls != 1 {
			t.Fatalf("expected marker writes=1, got %d", settingsRepo.setCalls)
		}
		if settingsRepo.values[bootstrapPlatformAdminSettingKey] == nil || settingsRepo.values[bootstrapPlatformAdminSettingKey].Value != userID.String() {
			t.Fatalf("expected marker value=%s, got %#v", userID, settingsRepo.values[bootstrapPlatformAdminSettingKey])
		}
	})

	t.Run("重复启动同 user_id 时幂等跳过", func(t *testing.T) {
		userID := uuid.New()
		credRepo := &stubBootstrapCredentialRepo{
			cred: &data.UserCredential{UserID: userID, Login: "alice"},
		}
		membershipRepo := &stubBootstrapMembershipRepo{}
		settingsRepo := &stubBootstrapSettingsRepo{
			values: map[string]*data.PlatformSetting{
				bootstrapPlatformAdminSettingKey: {Key: bootstrapPlatformAdminSettingKey, Value: userID.String()},
			},
		}

		err := bootstrapPlatformAdminOnce(context.Background(), credRepo, membershipRepo, settingsRepo, userID, logger)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if membershipRepo.calls != 0 {
			t.Fatalf("expected membership updates=0, got %d", membershipRepo.calls)
		}
		if settingsRepo.setCalls != 0 {
			t.Fatalf("expected marker writes=0, got %d", settingsRepo.setCalls)
		}
	})

	t.Run("标记存在但 user_id 不同则拒绝", func(t *testing.T) {
		userID := uuid.New()
		other := uuid.New()
		credRepo := &stubBootstrapCredentialRepo{
			cred: &data.UserCredential{UserID: userID, Login: "alice"},
		}
		membershipRepo := &stubBootstrapMembershipRepo{}
		settingsRepo := &stubBootstrapSettingsRepo{
			values: map[string]*data.PlatformSetting{
				bootstrapPlatformAdminSettingKey: {Key: bootstrapPlatformAdminSettingKey, Value: other.String()},
			},
		}

		err := bootstrapPlatformAdminOnce(context.Background(), credRepo, membershipRepo, settingsRepo, userID, logger)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if membershipRepo.calls != 0 {
			t.Fatalf("expected membership updates=0, got %d", membershipRepo.calls)
		}
		if settingsRepo.setCalls != 0 {
			t.Fatalf("expected marker writes=0, got %d", settingsRepo.setCalls)
		}
	})

	t.Run("用户不存在则失败且不写标记", func(t *testing.T) {
		userID := uuid.New()
		credRepo := &stubBootstrapCredentialRepo{}
		membershipRepo := &stubBootstrapMembershipRepo{}
		settingsRepo := &stubBootstrapSettingsRepo{}

		err := bootstrapPlatformAdminOnce(context.Background(), credRepo, membershipRepo, settingsRepo, userID, logger)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if membershipRepo.calls != 0 {
			t.Fatalf("expected membership updates=0, got %d", membershipRepo.calls)
		}
		if settingsRepo.setCalls != 0 {
			t.Fatalf("expected marker writes=0, got %d", settingsRepo.setCalls)
		}
	})
}

