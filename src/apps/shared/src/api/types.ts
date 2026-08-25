export type LoginRequest = {
  login: string
  password: string
}

export type LoginResponse = {
  token_type: string
  access_token: string
}
