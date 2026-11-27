// Код создан командой make types из структур Go (пакет api). Руками не править.

export interface AdminLoginStarted {
  id: string;
  code: string;
}

export interface AdminLoginState {
  status: string;
}

export interface AdminMe {
  tg_id: number;
  owner: boolean;
  perms: string[];
}

export interface ErrorBody {
  code: string;
  text: string;
}
