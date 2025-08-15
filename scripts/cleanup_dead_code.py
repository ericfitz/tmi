#!/usr/bin/env python3
"""
Dead Code Cleanup Script for OpenAPI Refactor

This script identifies and helps clean up dead code left behind from the OpenAPI refactor.
It provides analysis and optional automated cleanup of clearly dead code.

Usage: python3 scripts/cleanup_dead_code.py [--dry-run] [--auto-fix]
"""

import os
import re
import sys
from pathlib import Path
from typing import List, Tuple, Dict, Set
import argparse


class DeadCodeAnalyzer:
    """Analyzes and helps clean up dead code from OpenAPI refactor"""
    
    def __init__(self, project_root: str):
        self.project_root = Path(project_root)
        self.api_dir = self.project_root / "api"
        self.findings: Dict[str, List[Tuple[str, int, str]]] = {
            "commented_handlers": [],
            "deprecated_functions": [],
            "unused_imports": [],
            "todo_removals": [],
            "potential_dead_files": []
        }
    
    def analyze_commented_code(self):
        """Find commented out handler code and TODO removals"""
        server_file = self.api_dir / "server.go"
        if not server_file.exists():
            return
            
        with open(server_file, 'r') as f:
            lines = f.readlines()
        
        for i, line in enumerate(lines, 1):
            line_stripped = line.strip()
            
            # Find commented out handler code
            if re.search(r'//.*[Hh]andler.*(?:Disabled|Unused|TODO.*[Rr]emove)', line_stripped):
                self.findings["commented_handlers"].append((str(server_file), i, line.rstrip()))
            
            # Find TODO removal comments
            if re.search(r'//.*TODO.*[Rr]emove', line_stripped, re.IGNORECASE):
                self.findings["todo_removals"].append((str(server_file), i, line.rstrip()))
    
    def analyze_deprecated_functions(self):
        """Find deprecated wrapper functions"""
        for go_file in self.api_dir.glob("*.go"):
            if go_file.name.endswith("_test.go"):
                continue
                
            try:
                with open(go_file, 'r') as f:
                    content = f.read()
                    lines = content.split('\n')
            except Exception:
                continue
            
            # Look for deprecated function markers
            for i, line in enumerate(lines, 1):
                if re.search(r'//.*[Dd]eprecated', line):
                    # Look for the function definition on the next few lines
                    for j in range(i, min(i + 5, len(lines))):
                        if re.match(r'func\s+\w+', lines[j]):
                            func_name = re.search(r'func\s+(\w+)', lines[j])
                            if func_name:
                                self.findings["deprecated_functions"].append((
                                    str(go_file), i, f"Deprecated: {func_name.group(1)}"
                                ))
                            break
    
    def analyze_unused_handler_files(self):
        """Check for potentially unused handler files"""
        handler_files = list(self.api_dir.glob("*_handlers.go"))
        server_file = self.api_dir / "server.go"
        
        if not server_file.exists():
            return
            
        with open(server_file, 'r') as f:
            server_content = f.read()
        
        for handler_file in handler_files:
            handler_name = handler_file.stem  # e.g., "diagram_metadata_handlers"
            
            # Extract the likely handler struct name
            base_name = handler_name.replace("_handlers", "").replace("_", " ").title().replace(" ", "")
            possible_names = [
                f"{base_name}Handler",
                f"{handler_name}",
                f"New{base_name}Handler",
            ]
            
            # Check if any of these names appear in server.go
            referenced = False
            for name in possible_names:
                if name in server_content and not f"// {name}" in server_content:  # Not just commented
                    referenced = True
                    break
            
            if not referenced:
                # Additional check: see if it's used in tests or other files
                used_elsewhere = False
                for go_file in self.api_dir.glob("*.go"):
                    if go_file == handler_file:
                        continue
                    try:
                        with open(go_file, 'r') as f:
                            content = f.read()
                            for name in possible_names:
                                if name in content and not f"// {name}" in content:
                                    used_elsewhere = True
                                    break
                        if used_elsewhere:
                            break
                    except Exception:
                        continue
                
                if not used_elsewhere:
                    self.findings["potential_dead_files"].append((
                        str(handler_file), 1, f"Handler file potentially unused (checked for {possible_names})"
                    ))
    
    def analyze_unused_imports(self):
        """Find unused imports (basic analysis)"""
        for go_file in self.api_dir.glob("*.go"):
            try:
                with open(go_file, 'r') as f:
                    content = f.read()
                    
                # Simple check for imports that might be unused
                # This is a basic check - go lint would be more accurate
                import_section = re.search(r'import\s*\((.*?)\)', content, re.DOTALL)
                if import_section:
                    imports = import_section.group(1)
                    for line_num, line in enumerate(imports.split('\n'), 1):
                        line = line.strip().strip('"')
                        if line and not line.startswith('//'):
                            # Check for some obviously unused patterns
                            if 'unused' in line or 'deprecated' in line:
                                self.findings["unused_imports"].append((
                                    str(go_file), line_num, f"Potentially unused import: {line}"
                                ))
            except Exception:
                continue
    
    def generate_report(self) -> str:
        """Generate a comprehensive dead code report"""
        report = []
        report.append("# Dead Code Analysis Report")
        report.append("=" * 40)
        report.append("")
        
        total_issues = sum(len(findings) for findings in self.findings.values())
        report.append(f"**Total Issues Found: {total_issues}**")
        report.append("")
        
        categories = [
            ("commented_handlers", "🚫 Commented Out Handler Code", "High Priority - Safe to Remove"),
            ("deprecated_functions", "⚠️ Deprecated Functions", "Medium Priority - Check Usage First"),  
            ("todo_removals", "📝 TODO Removal Comments", "Low Priority - Review and Remove"),
            ("potential_dead_files", "📁 Potentially Dead Handler Files", "High Priority - Verify Before Removal"),
            ("unused_imports", "📦 Unused Imports", "Low Priority - Let Linter Handle")
        ]
        
        for key, title, priority in categories:
            findings = self.findings[key]
            if not findings:
                continue
                
            report.append(f"## {title}")
            report.append(f"**{priority}** - {len(findings)} issues found")
            report.append("")
            
            for file_path, line_num, description in findings:
                rel_path = os.path.relpath(file_path, self.project_root)
                report.append(f"- **{rel_path}:{line_num}** - {description}")
            
            report.append("")
        
        # Add cleanup recommendations
        report.append("## 🧹 Cleanup Recommendations")
        report.append("")
        report.append("### Automated Cleanup (Safe):")
        if self.findings["commented_handlers"]:
            report.append("- Remove commented out handler code lines")
        if self.findings["todo_removals"]:
            report.append("- Remove TODO removal comments")
            
        report.append("")
        report.append("### Manual Review Required:")
        if self.findings["potential_dead_files"]:
            report.append("- **CRITICAL:** Verify unused handler files before deletion")
            report.append("- Check if functionality was moved elsewhere or is still needed")
        if self.findings["deprecated_functions"]:
            report.append("- Review deprecated functions for remaining usage")
            report.append("- Update callers to use new implementations")
            
        report.append("")
        report.append("### Commands to Run:")
        report.append("```bash")
        report.append("# Run linter to catch unused imports")
        report.append("make run-lint")
        report.append("")
        report.append("# Run tests to ensure nothing breaks")
        report.append("make test-unit")
        report.append("make test-integration") 
        report.append("```")
        
        return "\n".join(report)
    
    def auto_fix_safe_issues(self, dry_run: bool = True):
        """Automatically fix issues that are safe to remove"""
        fixes_applied = []
        
        # Fix commented handler lines in server.go
        server_file = self.api_dir / "server.go"
        if server_file.exists() and self.findings["commented_handlers"]:
            with open(server_file, 'r') as f:
                lines = f.readlines()
            
            lines_to_remove = set()
            for file_path, line_num, description in self.findings["commented_handlers"]:
                if file_path == str(server_file):
                    # Only remove lines that are clearly safe (fully commented handlers)
                    line_content = lines[line_num - 1].strip()
                    if line_content.startswith('//') and 'handler' in line_content.lower():
                        lines_to_remove.add(line_num - 1)
            
            if lines_to_remove:
                new_lines = [line for i, line in enumerate(lines) if i not in lines_to_remove]
                
                if not dry_run:
                    with open(server_file, 'w') as f:
                        f.writelines(new_lines)
                
                fixes_applied.append(f"Removed {len(lines_to_remove)} commented handler lines from server.go")
        
        return fixes_applied
    
    def run_analysis(self, auto_fix: bool = False, dry_run: bool = True) -> str:
        """Run complete dead code analysis"""
        print("🔍 Analyzing dead code from OpenAPI refactor...")
        
        print("📝 Checking commented out handler code...")
        self.analyze_commented_code()
        
        print("⚠️ Checking deprecated functions...")
        self.analyze_deprecated_functions()
        
        print("📁 Checking for unused handler files...")
        self.analyze_unused_handler_files()
        
        print("📦 Checking for unused imports...")
        self.analyze_unused_imports()
        
        if auto_fix:
            print("🔧 Applying automatic fixes...")
            fixes = self.auto_fix_safe_issues(dry_run)
            for fix in fixes:
                print(f"  ✅ {fix}")
            if not fixes:
                print("  ℹ️ No safe automatic fixes available")
        
        print("📊 Generating report...")
        return self.generate_report()


def main():
    parser = argparse.ArgumentParser(description='Analyze and clean up dead code from OpenAPI refactor')
    parser.add_argument('--dry-run', action='store_true', help='Show what would be done without making changes')
    parser.add_argument('--auto-fix', action='store_true', help='Automatically fix safe issues')
    parser.add_argument('project_root', nargs='?', help='Project root directory (default: current directory)')
    
    args = parser.parse_args()
    
    if args.project_root:
        project_root = args.project_root
    else:
        # Try to find project root
        current_dir = Path.cwd()
        if (current_dir / "api" / "server.go").exists():
            project_root = str(current_dir)
        elif (current_dir.parent / "api" / "server.go").exists():
            project_root = str(current_dir.parent)
        else:
            print("Error: Could not find TMI project root. Please run from project root or specify path.")
            print("Usage: python3 scripts/cleanup_dead_code.py [--dry-run] [--auto-fix] [project_root]")
            sys.exit(1)
    
    print(f"🚀 Starting dead code analysis...")
    print(f"📁 Project root: {project_root}")
    
    analyzer = DeadCodeAnalyzer(project_root)
    report = analyzer.run_analysis(auto_fix=args.auto_fix, dry_run=args.dry_run)
    
    # Write report to file
    report_path = Path(project_root) / "dead_code_analysis_report.md"
    with open(report_path, 'w') as f:
        f.write(report)
    
    print(f"✅ Analysis complete! Report written to: {report_path}")
    print("\n" + "="*50)
    
    # Print summary to console
    lines = report.split('\n')
    for line in lines[:20]:  # Show first 20 lines of summary
        print(line)
    
    print("\nFor full report, see: dead_code_analysis_report.md")


if __name__ == "__main__":
    main()