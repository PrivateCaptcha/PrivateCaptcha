-- Revert organization_users table to original schema
-- This is a destructive migration - it will delete any email-only invites

-- Step 1: Delete records where user_id is NULL (email-only invites)
DELETE FROM backend.organization_users WHERE user_id IS NULL;

-- Step 2: Drop the check constraint
ALTER TABLE backend.organization_users DROP CONSTRAINT organization_users_user_or_email_check;

-- Step 3: Drop the unique indexes
DROP INDEX IF EXISTS backend.organization_users_org_user_idx;
DROP INDEX IF EXISTS backend.organization_users_org_email_idx;

-- Step 4: Drop the email column
ALTER TABLE backend.organization_users DROP COLUMN email;

-- Step 5: Make user_id NOT NULL again
ALTER TABLE backend.organization_users ALTER COLUMN user_id SET NOT NULL;

-- Step 6: Drop the id column
ALTER TABLE backend.organization_users DROP COLUMN id;

-- Step 7: Restore the original primary key
ALTER TABLE backend.organization_users ADD PRIMARY KEY (org_id, user_id);
