import { ApiError } from "./api";

export function pairErrorMessage(err: unknown): string {
  if (!(err instanceof ApiError)) {
    return "Pairing failed. Try again.";
  }
  switch (err.code) {
    case "unauthorized":
      return "That code is wrong or has expired. A code works once, for 5 minutes. Run agentws remote pair on the computer for a new one.";
    case "rate_limited":
      return "Too many wrong codes. Wait a minute, then try again.";
    case "network":
      return "Can't reach the server. Check that this device can reach it, then try again.";
    default:
      return "Pairing failed: " + err.message;
  }
}
