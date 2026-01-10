-- Add surrogate primary key and email column to organization_users table
-- First, drop the composite primary key and add a serial id as the new primary key
-- Also add email column for inviting non-existing users

-- Step 1: Drop the existing primary key constraint
ALTER TABLE backend.organization_users DROP CONSTRAINT organization_users_pkey;

-- Step 2: Add serial id column as the new primary key
ALTER TABLE backend.organization_users ADD COLUMN id SERIAL PRIMARY KEY;

-- Step 3: Make user_id nullable (for email-only invites)
ALTER TABLE backend.organization_users ALTER COLUMN user_id DROP NOT NULL;

-- Step 4: Add email column for inviting non-existing users
ALTER TABLE backend.organization_users ADD COLUMN email TEXT;

-- Step 5: Add a unique constraint on (org_id, user_id) where user_id is not null
-- This preserves the business logic that a user can only have one record per org
CREATE UNIQUE INDEX organization_users_org_user_idx ON backend.organization_users (org_id, user_id) WHERE user_id IS NOT NULL;

-- Step 6: Add a unique constraint on (org_id, email) where email is not null
-- This prevents duplicate invites for the same email in the same org
CREATE UNIQUE INDEX organization_users_org_email_idx ON backend.organization_users (org_id, email) WHERE email IS NOT NULL;

-- Step 7: Add a check constraint to ensure either user_id or email is set
ALTER TABLE backend.organization_users ADD CONSTRAINT organization_users_user_or_email_check 
CHECK (user_id IS NOT NULL OR email IS NOT NULL);
