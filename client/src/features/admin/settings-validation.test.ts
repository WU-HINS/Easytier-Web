/**
 * Field level validation for the console settings. The form around it needs a browser
 * and is exercised by scripts/ui-tour.mjs; the rules themselves are pure and are the
 * part that must not regress, because a wrong rule is saved without anyone noticing.
 */
import { describe, expect, it } from 'vitest';
import { validateSettings } from './settings-page';
import { copy as zhCN } from '../../copy/zh-CN';
import type { AdminSettings } from '../../lib/http';

const valid: AdminSettings = {
  max_instances_per_user: 10,
  password_min_length: 10,
  password_min_classes: 3,
  reserved_usernames: 'root,admin',
  background_url: '',
  registration_open: true,
  oidc_enabled: false,
  oidc_issuer: '',
  oidc_client_id: '',
  oidc_client_secret_set: false,
  oidc_button_label: '',
  extra_tcp_enabled: false,
  extra_udp_enabled: false,
  extra_port: 0,
  revision: 1,
};

describe('validateSettings', () => {
  it('accepts a coherent form', () => {
    expect(validateSettings(valid, zhCN)).toEqual({});
  });

  it('rejects a negative instance quota', () => {
    expect(validateSettings({ ...valid, max_instances_per_user: -1 }, zhCN).max_instances_per_user).toBeDefined();
  });

  it('bounds the password length to the range the server enforces', () => {
    expect(validateSettings({ ...valid, password_min_length: 5 }, zhCN).password_min_length).toBeDefined();
    expect(validateSettings({ ...valid, password_min_length: 129 }, zhCN).password_min_length).toBeDefined();
  });

  it('rejects reserved names that are not a plain login name', () => {
    expect(validateSettings({ ...valid, reserved_usernames: 'ok,not ok' }, zhCN).reserved_usernames).toBeDefined();
  });

  it('rejects an extra listener port outside 0 and 1024-65535', () => {
    expect(validateSettings({ ...valid, extra_port: 80 }, zhCN).extra_port).toBeDefined();
  });
});
