import type { Candidate } from './features/Composer.mentions';
import type { T } from './api';
export function collectiveOptions(people: Candidate[], view: T.TeamsView | null): Candidate[];

export interface CollectiveRef { kind: 'person' | 'guest' | 'agent'; id: string }
export const collectivePrefix: string;
export const collectiveRef: RegExp;
export function parseCollective(href: unknown): CollectiveRef[] | null;
export function collectiveHref(refs: CollectiveRef[]): string | null;
export function collectivePlain(text: string | undefined): string;
