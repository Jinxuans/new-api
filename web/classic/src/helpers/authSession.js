/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

export function getStoredUser() {
  const raw = localStorage.getItem('user');
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch (error) {
    localStorage.removeItem('user');
    return null;
  }
}

export function normalizeUserData(data) {
  if (!data || typeof data !== 'object') return data;

  const currentUser = getStoredUser();
  const user = data.user || currentUser;
  if (!data.access_token || !user) return data;

  return {
    ...user,
    token: data.access_token,
    access_token: data.access_token,
    token_type: data.token_type || 'Bearer',
    access_expires_at: data.access_expires_at,
    auth_session: data.session || user.auth_session,
  };
}

export function storeUserData(data) {
  const user = normalizeUserData(data);
  if (user) {
    localStorage.setItem('user', JSON.stringify(user));
  }
  return user;
}

export function getDashboardAccessToken() {
  const user = getStoredUser();
  return user?.access_token || user?.token || '';
}

export function getDashboardSessionId() {
  return getStoredUser()?.auth_session?.sid || '';
}

export function clearUserData() {
  localStorage.removeItem('user');
}
