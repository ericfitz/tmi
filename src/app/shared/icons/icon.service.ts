import { library } from '@fortawesome/fontawesome-svg-core';

// Import only the specific Regular icons we need
import { 
  faFile,
  faFolderOpen,
  faFloppyDisk,
  faCopy,
  faPaste
} from '@fortawesome/free-regular-svg-icons';

// Import solid icons for features not available in regular
import {
  faFileExport,
  faXmark,
  faHouse,
  faRightToBracket,
  faRightFromBracket
} from '@fortawesome/free-solid-svg-icons';

// Import brand icons for Google-related features
import {
  faGoogle,
  faGoogleDrive
} from '@fortawesome/free-brands-svg-icons';

/**
 * Registers all Font Awesome icons used by the application.
 * This function should be called during application initialization.
 */
export function registerIcons(): void {
  // Add the icons to the library
  library.add(
    // Regular icons
    faFile,
    faFolderOpen,
    faFloppyDisk,
    faCopy,
    faPaste,
    
    // Solid icons
    faFileExport,
    faXmark,
    faHouse,
    faRightToBracket,
    faRightFromBracket,
    
    // Brand icons
    faGoogle,
    faGoogleDrive
  );
}