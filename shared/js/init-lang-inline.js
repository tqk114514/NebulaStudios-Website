(function () {
  var LANG_NAMES = {
    'zh-CN': '简体中文',
    'zh-TW': '繁體中文',
    'en': 'English',
    'ja': '日本語',
    'ko': '한국어'
  };
  var validLangs = Object.keys(LANG_NAMES);

  // 内联 getCookie（此脚本直接注入 HTML <script> 标签，无法 import 任何模块）
  // 重复实现是有意为之，详见 shared/js/utils/cookie.ts 中的完整版本
function getCookie(name) {
    var match = document.cookie.match('(?:^|;)\\s*' + name + '=([^;]*)');
    return match ? match[1] : null;
  }

  var lang = getCookie('selectedLanguage');
  if (!lang || !LANG_NAMES[lang]) {
    var bl = navigator.language || '';
    if (bl.startsWith('zh')) {
      lang = (bl.includes('TW') || bl.includes('HK')) ? 'zh-TW' : 'zh-CN';
    } else if (bl.startsWith('ja')) {
      lang = 'ja';
    } else if (bl.startsWith('ko')) {
      lang = 'ko';
    } else if (bl.startsWith('en')) {
      lang = 'en';
    } else {
      lang = 'en';
    }
  }

  document.documentElement.lang = lang;
  // 标记 JS 可用：CSS 依据 html.js 做"仅 JS 时隐藏待揭示内容"等渐进增强门控，
  // JS 禁用时内容保持默认可见
  document.documentElement.classList.add('js');
  if (lang !== 'zh-CN') {
    document.documentElement.style.visibility = 'hidden';
  }

  window.__INIT_LANG__ = lang;
})();
