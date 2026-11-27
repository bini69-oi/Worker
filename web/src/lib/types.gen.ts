// Код создан командой make types из структур Go (пакет api). Руками не править.

export interface ErrorBody {
  code: string;
  text: string;
}

export interface LoginPollResponse {
  state: string;
}

export interface LoginStartResponse {
  id: string;
  code: string;
  link: string;
  expires_at: string;
}

export interface SessionResponse {
  token: string;
  lang: string;
  expires_at: string;
}
