export function shouldDelayLocalSession(localMode: boolean, sidecarReady: boolean, onboardingDone: boolean | null): boolean {
  return localMode && (!sidecarReady || onboardingDone !== true)
}
