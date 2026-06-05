package db

import (
	"context"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jackc/pgx/v5/pgtype"
)

type QuerierStub struct {
	Error error
}

var _ dbgen.Querier = (*QuerierStub)(nil)

func (s *QuerierStub) CreateAPIKey(ctx context.Context, arg *dbgen.CreateAPIKeyParams) (*dbgen.APIKey, error) {
	return nil, s.Error
}
func (s *QuerierStub) CreateAsyncTask(ctx context.Context, arg *dbgen.CreateAsyncTaskParams) (pgtype.UUID, error) {
	return pgtype.UUID{}, s.Error
}
func (s *QuerierStub) CreateAuditLogs(ctx context.Context, arg []*dbgen.CreateAuditLogsParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) CreateCache(ctx context.Context, arg *dbgen.CreateCacheParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) CreateCacheMany(ctx context.Context, arg *dbgen.CreateCacheManyParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) CreateDifficultyRule(ctx context.Context, arg *dbgen.CreateDifficultyRuleParams) (*dbgen.DifficultyRule, error) {
	return nil, s.Error
}
func (s *QuerierStub) CreateForm(ctx context.Context, arg *dbgen.CreateFormParams) (*dbgen.Form, error) {
	return nil, s.Error
}
func (s *QuerierStub) CreateNotificationTemplate(ctx context.Context, arg *dbgen.CreateNotificationTemplateParams) (*dbgen.NotificationTemplate, error) {
	return nil, s.Error
}
func (s *QuerierStub) CreateOrganization(ctx context.Context, arg *dbgen.CreateOrganizationParams) (*dbgen.Organization, error) {
	return nil, s.Error
}
func (s *QuerierStub) CreateProperty(ctx context.Context, arg *dbgen.CreatePropertyParams) (*dbgen.Property, error) {
	return nil, s.Error
}
func (s *QuerierStub) CreateSubscription(ctx context.Context, arg *dbgen.CreateSubscriptionParams) (*dbgen.Subscription, error) {
	return nil, s.Error
}
func (s *QuerierStub) CreateSystemNotification(ctx context.Context, arg *dbgen.CreateSystemNotificationParams) (*dbgen.SystemNotification, error) {
	return nil, s.Error
}
func (s *QuerierStub) CreateUser(ctx context.Context, arg *dbgen.CreateUserParams) (*dbgen.User, error) {
	return nil, s.Error
}
func (s *QuerierStub) CreateUserNotification(ctx context.Context, arg *dbgen.CreateUserNotificationParams) (*dbgen.UserNotification, error) {
	return nil, s.Error
}
func (s *QuerierStub) DeactivateForms(ctx context.Context, dollar_1 []int32) ([]*dbgen.Form, error) {
	return nil, s.Error
}
func (s *QuerierStub) DeleteAPIKey(ctx context.Context, arg *dbgen.DeleteAPIKeyParams) (*dbgen.APIKey, error) {
	return nil, s.Error
}
func (s *QuerierStub) DeleteCachedByKey(ctx context.Context, key string) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeleteDeletedRecords(ctx context.Context, deletedAt pgtype.Timestamptz) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeleteDifficultyRule(ctx context.Context, arg *dbgen.DeleteDifficultyRuleParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeleteExpiredCache(ctx context.Context) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeleteForms(ctx context.Context, dollar_1 []int32) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeleteLock(ctx context.Context, name string) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeleteOldAsyncTasks(ctx context.Context, createdAt pgtype.Timestamptz) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeleteOldAuditLogs(ctx context.Context, createdAt pgtype.Timestamptz) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeleteOrganizations(ctx context.Context, dollar_1 []int32) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeletePendingUserNotification(ctx context.Context, arg *dbgen.DeletePendingUserNotificationParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeleteProcessedUserNotifications(ctx context.Context, processedAt pgtype.Timestamptz) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeleteProperties(ctx context.Context, dollar_1 []int32) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeleteUnprocessedUserNotifications(ctx context.Context, scheduledAt pgtype.Timestamptz) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeleteUnusedNotificationTemplates(ctx context.Context, arg *dbgen.DeleteUnusedNotificationTemplatesParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) DeleteUserAPIKeys(ctx context.Context, userID pgtype.Int4) ([]pgtype.UUID, error) {
	return nil, s.Error
}
func (s *QuerierStub) DeleteUsers(ctx context.Context, dollar_1 []int32) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) FindUserOrgByName(ctx context.Context, arg *dbgen.FindUserOrgByNameParams) (*dbgen.Organization, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetAPIKeyByExternalID(ctx context.Context, externalID pgtype.UUID) (*dbgen.APIKey, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetAsyncTask(ctx context.Context, id pgtype.UUID) (*dbgen.AsyncTask, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetCachedByKey(ctx context.Context, key string) ([]byte, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetDifficultyRuleByID(ctx context.Context, id int32) (*dbgen.DifficultyRule, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetDifficultyRulePositionNeighbors(ctx context.Context, arg *dbgen.GetDifficultyRulePositionNeighborsParams) (*dbgen.GetDifficultyRulePositionNeighborsRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetDifficultyRulesByOrgIDs(ctx context.Context, dollar_1 []int32) ([]*dbgen.DifficultyRule, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetDifficultyRulesByPropertyIDs(ctx context.Context, dollar_1 []int32) ([]*dbgen.DifficultyRule, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetFormByExternalID(ctx context.Context, externalID pgtype.UUID) (*dbgen.Form, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetFormByID(ctx context.Context, formID int32) (*dbgen.Form, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetFormByPropertyID(ctx context.Context, propertyID int32) (*dbgen.Form, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetFormsByID(ctx context.Context, dollar_1 []int32) ([]*dbgen.Form, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetOrgForms(ctx context.Context, arg *dbgen.GetOrgFormsParams) ([]*dbgen.Form, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetOrgFormsCount(ctx context.Context, orgID pgtype.Int4) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) GetFormsByExternalID(ctx context.Context, dollar_1 []pgtype.UUID) ([]*dbgen.Form, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetLastActiveSystemNotification(ctx context.Context, arg *dbgen.GetLastActiveSystemNotificationParams) (*dbgen.SystemNotification, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetLock(ctx context.Context, name string) (*dbgen.Lock, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetNotificationTemplateByHash(ctx context.Context, externalID string) (*dbgen.NotificationTemplate, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetOrgAuditLogs(ctx context.Context, arg *dbgen.GetOrgAuditLogsParams) ([]*dbgen.GetOrgAuditLogsRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetFormAuditLogs(ctx context.Context, arg *dbgen.GetFormAuditLogsParams) ([]*dbgen.GetFormAuditLogsRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetOrgProperties(ctx context.Context, arg *dbgen.GetOrgPropertiesParams) ([]*dbgen.Property, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetOrgPropertiesCount(ctx context.Context, orgID pgtype.Int4) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) GetOrgPropertyByName(ctx context.Context, arg *dbgen.GetOrgPropertyByNameParams) (*dbgen.Property, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetOrgFormByName(ctx context.Context, arg *dbgen.GetOrgFormByNameParams) (*dbgen.Form, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetOrganizationUsers(ctx context.Context, orgID int32) ([]*dbgen.GetOrganizationUsersRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetOrganizationUsersWithEmailInvites(ctx context.Context, orgID int32) ([]*dbgen.GetOrganizationUsersWithEmailInvitesRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetOrganizationWithAccess(ctx context.Context, arg *dbgen.GetOrganizationWithAccessParams) (*dbgen.GetOrganizationWithAccessRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetPendingAsyncTasks(ctx context.Context, arg *dbgen.GetPendingAsyncTasksParams) ([]*dbgen.GetPendingAsyncTasksRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetPendingUserNotifications(ctx context.Context, arg *dbgen.GetPendingUserNotificationsParams) ([]*dbgen.GetPendingUserNotificationsRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetProperties(ctx context.Context, limit int32) ([]*dbgen.Property, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetPropertiesByExternalID(ctx context.Context, dollar_1 []pgtype.UUID) ([]*dbgen.Property, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetPropertiesByID(ctx context.Context, dollar_1 []int32) ([]*dbgen.Property, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetPropertyAccessViolations(ctx context.Context, arg *dbgen.GetPropertyAccessViolationsParams) ([]int32, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetPropertyAuditLogs(ctx context.Context, arg *dbgen.GetPropertyAuditLogsParams) ([]*dbgen.GetPropertyAuditLogsRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetPropertyByExternalID(ctx context.Context, externalID pgtype.UUID) (*dbgen.Property, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetPropertyByID(ctx context.Context, id int32) (*dbgen.Property, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetSoftDeletedOrganizations(ctx context.Context, arg *dbgen.GetSoftDeletedOrganizationsParams) ([]*dbgen.GetSoftDeletedOrganizationsRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetSoftDeletedProperties(ctx context.Context, arg *dbgen.GetSoftDeletedPropertiesParams) ([]*dbgen.GetSoftDeletedPropertiesRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetSoftDeletedForms(ctx context.Context, arg *dbgen.GetSoftDeletedFormsParams) ([]*dbgen.GetSoftDeletedFormsRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetSoftDeletedUsers(ctx context.Context, arg *dbgen.GetSoftDeletedUsersParams) ([]*dbgen.GetSoftDeletedUsersRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetSubscriptionByID(ctx context.Context, id int32) (*dbgen.Subscription, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetSystemNotificationById(ctx context.Context, id int32) (*dbgen.SystemNotification, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetUserAPIKeyByName(ctx context.Context, arg *dbgen.GetUserAPIKeyByNameParams) (*dbgen.APIKey, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetUserAPIKeys(ctx context.Context, userID pgtype.Int4) ([]*dbgen.APIKey, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetUserAuditLogs(ctx context.Context, arg *dbgen.GetUserAuditLogsParams) ([]*dbgen.GetUserAuditLogsRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetUserByEmail(ctx context.Context, lower string) (*dbgen.User, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetUserByID(ctx context.Context, id int32) (*dbgen.User, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetUserOrganizations(ctx context.Context, userID pgtype.Int4) ([]*dbgen.GetUserOrganizationsRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetUserPropertiesCount(ctx context.Context, userID pgtype.Int4) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) GetUserSettings(ctx context.Context, userID int32) (*dbgen.UserSettings, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetUsersWithPendingMonthlyReport(ctx context.Context, arg *dbgen.GetUsersWithPendingMonthlyReportParams) ([]*dbgen.GetUsersWithPendingMonthlyReportRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetUsersWithPendingWeeklyReport(ctx context.Context, arg *dbgen.GetUsersWithPendingWeeklyReportParams) ([]*dbgen.GetUsersWithPendingWeeklyReportRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetUsersWithoutSubscription(ctx context.Context, dollar_1 []int32) ([]*dbgen.User, error) {
	return nil, s.Error
}
func (s *QuerierStub) InsertLock(ctx context.Context, arg *dbgen.InsertLockParams) (*dbgen.Lock, error) {
	return nil, s.Error
}
func (s *QuerierStub) InviteEmailToOrg(ctx context.Context, arg *dbgen.InviteEmailToOrgParams) (*dbgen.OrganizationUser, error) {
	return nil, s.Error
}
func (s *QuerierStub) InviteUserToOrg(ctx context.Context, arg *dbgen.InviteUserToOrgParams) (*dbgen.OrganizationUser, error) {
	return nil, s.Error
}
func (s *QuerierStub) LinkOrgInviteToUser(ctx context.Context, arg *dbgen.LinkOrgInviteToUserParams) (*dbgen.OrganizationUser, error) {
	return nil, s.Error
}
func (s *QuerierStub) MoveDifficultyRule(ctx context.Context, arg *dbgen.MoveDifficultyRuleParams) (*dbgen.DifficultyRule, error) {
	return nil, s.Error
}
func (s *QuerierStub) MoveProperty(ctx context.Context, arg *dbgen.MovePropertyParams) (*dbgen.Property, error) {
	return nil, s.Error
}
func (s *QuerierStub) MovePropertyWithForm(ctx context.Context, arg *dbgen.MovePropertyWithFormParams) (*dbgen.Property, error) {
	return nil, s.Error
}
func (s *QuerierStub) MoveForm(ctx context.Context, arg *dbgen.MoveFormParams) (*dbgen.Form, error) {
	return nil, s.Error
}
func (s *QuerierStub) SoftDeleteForm(ctx context.Context, id int32) (*dbgen.Form, error) {
	return nil, s.Error
}
func (s *QuerierStub) Ping(ctx context.Context) (int32, error) {
	return 0, s.Error
}
func (s *QuerierStub) RebalanceDifficultyRules(ctx context.Context, arg *dbgen.RebalanceDifficultyRulesParams) ([]int32, error) {
	return nil, s.Error
}
func (s *QuerierStub) RemoveUnlinkedOrgInviteByID(ctx context.Context, id int32) (pgtype.Text, error) {
	return pgtype.Text{}, s.Error
}
func (s *QuerierStub) RemoveUserFromOrg(ctx context.Context, arg *dbgen.RemoveUserFromOrgParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) RotateAPIKey(ctx context.Context, arg *dbgen.RotateAPIKeyParams) (*dbgen.APIKey, error) {
	return nil, s.Error
}
func (s *QuerierStub) SoftDeleteProperties(ctx context.Context, arg *dbgen.SoftDeletePropertiesParams) ([]*dbgen.Property, error) {
	return nil, s.Error
}
func (s *QuerierStub) SoftDeleteProperty(ctx context.Context, id int32) (*dbgen.Property, error) {
	return nil, s.Error
}
func (s *QuerierStub) SoftDeletePropertyWithForm(ctx context.Context, id int32) (*dbgen.Property, error) {
	return nil, s.Error
}
func (s *QuerierStub) SoftDeleteUser(ctx context.Context, id int32) (*dbgen.User, error) {
	return nil, s.Error
}
func (s *QuerierStub) SoftDeleteUserOrganization(ctx context.Context, arg *dbgen.SoftDeleteUserOrganizationParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) SoftDeleteUserOrganizations(ctx context.Context, userID pgtype.Int4) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) SwapOrgOwnership(ctx context.Context, arg *dbgen.SwapOrgOwnershipParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) TransferOrgProperties(ctx context.Context, arg *dbgen.TransferOrgPropertiesParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) TransferOrgForms(ctx context.Context, arg *dbgen.TransferOrgFormsParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) TransferOrganization(ctx context.Context, arg *dbgen.TransferOrganizationParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) UpdateAPIKey(ctx context.Context, arg *dbgen.UpdateAPIKeyParams) (*dbgen.APIKey, error) {
	return nil, s.Error
}
func (s *QuerierStub) UpdateAPIKeysLastUsedAt(ctx context.Context, dollar_1 []int32) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) UpdateAsyncTask(ctx context.Context, arg *dbgen.UpdateAsyncTaskParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) UpdateAttemptedUserNotifications(ctx context.Context, dollar_1 []int32) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) UpdateCacheExpiration(ctx context.Context, arg *dbgen.UpdateCacheExpirationParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) UpdateDifficultyRule(ctx context.Context, arg *dbgen.UpdateDifficultyRuleParams) (*dbgen.UpdateDifficultyRuleRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) UpdateForm(ctx context.Context, arg *dbgen.UpdateFormParams) (*dbgen.UpdateFormRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) UpdateInternalSubscriptions(ctx context.Context, arg *dbgen.UpdateInternalSubscriptionsParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) UpdateOrgMembershipLevel(ctx context.Context, arg *dbgen.UpdateOrgMembershipLevelParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) UpdateOrganization(ctx context.Context, arg *dbgen.UpdateOrganizationParams) (*dbgen.Organization, error) {
	return nil, s.Error
}
func (s *QuerierStub) UpdateProcessedUserNotifications(ctx context.Context, arg *dbgen.UpdateProcessedUserNotificationsParams) (int64, error) {
	return 0, s.Error
}
func (s *QuerierStub) UpdateProperty(ctx context.Context, arg *dbgen.UpdatePropertyParams) (*dbgen.UpdatePropertyRow, error) {
	return nil, s.Error
}
func (s *QuerierStub) UpdateUserData(ctx context.Context, arg *dbgen.UpdateUserDataParams) (*dbgen.User, error) {
	return nil, s.Error
}
func (s *QuerierStub) UpdateUserSubscription(ctx context.Context, arg *dbgen.UpdateUserSubscriptionParams) (*dbgen.User, error) {
	return nil, s.Error
}
func (s *QuerierStub) UpsertUserSettings(ctx context.Context, arg *dbgen.UpsertUserSettingsParams) (*dbgen.UserSettings, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetPropertyEditViolations(ctx context.Context, arg *dbgen.GetPropertyAccessViolationsParams) ([]int32, error) {
	return nil, s.Error
}
func (s *QuerierStub) GetUserFormsCount(ctx context.Context, userID pgtype.Int4) (int64, error) {
	return 0, s.Error
}
