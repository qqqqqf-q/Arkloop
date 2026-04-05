import { loader } from 'fumadocs-core/source';
import { docsZh, docsEn, apiZh, apiEn } from '../../.source';

export const docsZhSource = loader({
  baseUrl: '/zh/docs',
  source: docsZh.toFumadocsSource(),
});

export const docsEnSource = loader({
  baseUrl: '/en/docs',
  source: docsEn.toFumadocsSource(),
});

export const apiZhSource = loader({
  baseUrl: '/zh/api',
  source: apiZh.toFumadocsSource(),
});

export const apiEnSource = loader({
  baseUrl: '/en/api',
  source: apiEn.toFumadocsSource(),
});
