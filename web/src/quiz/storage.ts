// The student's id lives in the "user_id" cookie, so a reload continues the quiz.
export function setCookie(name: string, value: string, days = 30) {
  const expires = new Date();
  expires.setTime(expires.getTime() + days * 24 * 60 * 60 * 1000);
  document.cookie = name + '=' + value + ';expires=' + expires.toUTCString() + ';path=/';
}

export function getCookie(name: string): string | null {
  const prefix = name + '=';
  const parts = document.cookie.split(';');
  for (let i = 0; i < parts.length; i++) {
    let part = parts[i];
    while (part.charAt(0) === ' ') part = part.substring(1);
    if (part.indexOf(prefix) === 0) return part.substring(prefix.length);
  }
  return null;
}

// The registration form is filled with what the student typed last time.
const REGISTRATION_STORAGE_KEY = 'webquiz_registration_data';

export function saveRegistrationData(data: Record<string, unknown>) {
  try {
    localStorage.setItem(REGISTRATION_STORAGE_KEY, JSON.stringify(data));
  } catch (error) {
    console.log('Failed to save registration data to localStorage:', error);
  }
}

export function loadRegistrationData(): Record<string, unknown> | null {
  try {
    const stored = localStorage.getItem(REGISTRATION_STORAGE_KEY);
    return stored ? (JSON.parse(stored) as Record<string, unknown>) : null;
  } catch (error) {
    console.log('Failed to load registration data from localStorage:', error);
    return null;
  }
}
