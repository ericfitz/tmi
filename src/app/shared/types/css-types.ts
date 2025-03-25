/**
 * Type definitions for CSS class names to ensure consistency and follow BEM methodology
 * https://getbem.com/
 */

// Block-Element-Modifier (BEM) pattern types
export type BlockName = 
  | 'app'
  | 'header'
  | 'footer'
  | 'landing'
  | 'diagram'
  | 'toolbar'
  | 'login'
  | 'language-selector'
  | 'button'
  | string;

export type ElementName = 
  | 'content'
  | 'title'
  | 'subtitle'
  | 'container'
  | 'left'
  | 'right'
  | 'center'
  | 'nav'
  | 'logo'
  | 'text'
  | string;

export type ModifierName = 
  | 'primary'
  | 'secondary'
  | 'active'
  | 'disabled'
  | 'hidden'
  | 'visible'
  | 'rtl'
  | 'ltr'
  | string;

/**
 * Helper function to construct BEM class names
 * 
 * @example
 * // Returns 'header__logo'
 * bemClass('header', 'logo');
 * 
 * @example
 * // Returns 'button button--primary'
 * bemClass('button', null, 'primary');
 * 
 * @example
 * // Returns 'header__nav header__nav--active'
 * bemClass('header', 'nav', 'active'); 
 */
export function bemClass(
  block: BlockName,
  element?: ElementName | null,
  modifier?: ModifierName | null
): string {
  let className = block;
  
  if (element) {
    className = `${block}__${element}`;
  }
  
  if (modifier) {
    return `${className} ${className}--${modifier}`;
  }
  
  return className;
}

/**
 * Type definition for utility classes
 */
export type UtilityClass = 
  | 'hidden'
  | 'visible'
  | 'flex'
  | 'block'
  | 'grid'
  | 'flex-row'
  | 'flex-column'
  | 'justify-center'
  | 'align-center'
  | 'text-center'
  | 'text-left'
  | 'text-right'
  | 'margin-top'
  | 'margin-bottom'
  | 'margin-left'
  | 'margin-right'
  | 'padding-top'
  | 'padding-bottom'
  | 'padding-left'
  | 'padding-right'
  | string;