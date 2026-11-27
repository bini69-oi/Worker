// Код создан командой make types из структур Go (пакет api). Руками не править.

export interface ErrorBody {
  code: string;
  text: string;
}

export interface SessionResponse {
  token: string;
  lang: string;
  expires_at: string;
}
