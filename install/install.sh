#!/bin/bash
# SPDX-License-Identifier: AGPL-3.0-or-later

# Ackify Community Edition (CE) Installation Script
# Interactive setup for Docker deployment

set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Helper functions
print_header() {
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

prompt_input() {
    local prompt="$1"
    local default="$2"
    local response

    if [ -n "$default" ]; then
        read -p "$(echo -e ${BLUE}"$prompt [${default}]: "${NC})" response
        echo "${response:-$default}"
    else
        read -p "$(echo -e ${BLUE}"$prompt: "${NC})" response
        echo "$response"
    fi
}

prompt_yes_no() {
    local prompt="$1"
    local default="$2"
    local response

    if [ "$default" = "y" ]; then
        read -p "$(echo -e ${BLUE}"$prompt [Y/n]: "${NC})" response
        response="${response:-y}"
    else
        read -p "$(echo -e ${BLUE}"$prompt [y/N]: "${NC})" response
        response="${response:-n}"
    fi

    [[ "$response" =~ ^[Yy]$ ]]
}

prompt_password() {
    local prompt="$1"
    local response

    read -sp "$(echo -e ${BLUE}"$prompt: "${NC})" response
    echo "" >&2
    echo "$response"
}

# Template region processing functions
# Remove a region entirely (markers + content)
remove_region() {
    local file="$1"
    local region="$2"
    sed -i "/#BEGIN:${region}/,/#END:${region}/d" "$file"
}

# Keep a region (remove only the markers, keep content)
keep_region() {
    local file="$1"
    local region="$2"
    sed -i "/#BEGIN:${region}/d;/#END:${region}/d" "$file"
}

# Clean any remaining region markers (safety cleanup)
clean_all_markers() {
    local file="$1"
    sed -i '/#BEGIN:/d;/#END:/d' "$file"
}

# ==========================================
# Update mode functions
# ==========================================

# Associative array to store existing env values
declare -A ENV_VALUES

# Load existing .env file into ENV_VALUES array
load_env_file() {
    local env_file="$1"
    if [ -f "$env_file" ]; then
        while IFS= read -r line || [ -n "$line" ]; do
            # Skip empty lines and comments
            [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
            # Parse KEY=VALUE (handle values with = in them)
            if [[ "$line" =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]]; then
                local key="${BASH_REMATCH[1]}"
                local value="${BASH_REMATCH[2]}"
                # Remove surrounding quotes if present
                value="${value#\"}"
                value="${value%\"}"
                value="${value#\'}"
                value="${value%\'}"
                ENV_VALUES["$key"]="$value"
            fi
        done < "$env_file"
    fi
}

# Get value from existing .env (returns empty if not found)
get_env() {
    local key="$1"
    echo "${ENV_VALUES[$key]:-}"
}

# Check if a variable exists and is non-empty in existing .env
has_env() {
    local key="$1"
    [[ -n "${ENV_VALUES[$key]:-}" ]]
}

# Check if OAuth is configured
is_oauth_configured() {
    has_env "ACKIFY_OAUTH_CLIENT_ID" && has_env "ACKIFY_OAUTH_CLIENT_SECRET"
}

# Check if SMTP is configured
is_smtp_configured() {
    has_env "ACKIFY_MAIL_HOST"
}

# Check if Traefik is configured
is_traefik_configured() {
    has_env "TRAEFIK_NETWORK"
}

# Check if Storage is configured
is_storage_configured() {
    has_env "ACKIFY_STORAGE_TYPE"
}

# Add or update a variable in the .env file
set_env_in_file() {
    local file="$1"
    local key="$2"
    local value="$3"

    if grep -q "^${key}=" "$file" 2>/dev/null; then
        # Update existing
        sed -i "s|^${key}=.*|${key}=${value}|" "$file"
    else
        # Add new
        echo "${key}=${value}" >> "$file"
    fi
}

# Add a section header if not present
add_section_header() {
    local file="$1"
    local header="$2"
    if ! grep -q "^# ${header}$" "$file" 2>/dev/null; then
        echo "" >> "$file"
        echo "# ==========================================" >> "$file"
        echo "# ${header}" >> "$file"
        echo "# ==========================================" >> "$file"
    fi
}

# Main installation
clear
print_header "🔐 Ackify Community Edition (CE) - Interactive Setup"

echo ""

# Detect installation vs update mode
INSTALL_DIR="ackify-ce"
UPDATE_MODE=false

if [ -d "$INSTALL_DIR" ]; then
    if [ -f "$INSTALL_DIR/.env" ]; then
        # Existing installation detected
        print_info "Existing Ackify installation detected in $INSTALL_DIR"
        echo ""
        if prompt_yes_no "Update existing installation?" "y"; then
            UPDATE_MODE=true
            cd "$INSTALL_DIR"
            load_env_file ".env"
            print_success "Update mode activated - existing configuration loaded"
            print_info "Only unconfigured features will be prompted."
        else
            print_error "Installation cancelled"
            exit 1
        fi
    else
        # Directory exists but no .env
        print_error "Directory $INSTALL_DIR exists but has no .env file."
        if prompt_yes_no "Remove existing directory and start fresh?" "n"; then
            rm -rf "$INSTALL_DIR"
            mkdir -p "$INSTALL_DIR"
            cd "$INSTALL_DIR"
            print_success "Fresh installation directory created"
        else
            print_error "Installation cancelled"
            exit 1
        fi
    fi
else
    # New installation
    mkdir -p "$INSTALL_DIR"
    cd "$INSTALL_DIR"
    print_success "Installation directory created: $(pwd)"
fi

echo ""
if [ "$UPDATE_MODE" = true ]; then
    print_info "Running in UPDATE mode - preserving existing configuration."
else
    print_info "Running in INSTALL mode - fresh installation."
    print_info "At least ONE authentication method (OAuth or MagicLink) must be enabled."
fi
echo ""

# ==========================================
# Basic Configuration
# ==========================================
if [ "$UPDATE_MODE" = true ] && has_env "ACKIFY_BASE_URL"; then
    # Use existing values in update mode
    APP_BASE_URL=$(get_env "ACKIFY_BASE_URL")
    APP_ORGANISATION=$(get_env "ACKIFY_ORGANISATION")
    APP_ORGANISATION_DOMAIN=$(get_env "ACKIFY_ORGANISATION_DOMAIN")
    print_header "🌐 Basic Configuration (existing)"
    print_success "Base URL: ${APP_BASE_URL}"
    print_success "Organization: ${APP_ORGANISATION}"
    if [ -n "$APP_ORGANISATION_DOMAIN" ]; then
        print_success "Organization Domain: ${APP_ORGANISATION_DOMAIN}"
    fi
else
    print_header "🌐 Basic Configuration"
    echo ""
    APP_BASE_URL=$(prompt_input "Application Base URL (e.g., https://ackify.example.com)" "http://localhost:8080")
    APP_ORGANISATION=$(prompt_input "Organization Name" "My Organization")
    APP_ORGANISATION_DOMAIN=$(prompt_input "Organization Email Domain (e.g., company.com, leave empty to allow all)" "")
    print_success "Base configuration set"
fi

APP_DNS=$(echo "$APP_BASE_URL" | sed -E 's|https?://||')

# Extract domain for email addresses (remove subdomain if present, remove port)
APP_DOMAIN=$(echo "$APP_DNS" | sed 's/:.*//')
# Count dots to detect subdomain
dot_count=$(echo "$APP_DOMAIN" | tr -cd '.' | wc -c)
# If 2+ dots (subdomain.domain.tld), remove first part to keep domain.tld
if [ "$dot_count" -ge 2 ]; then
    APP_DOMAIN=$(echo "$APP_DOMAIN" | cut -d. -f2-)
fi
echo ""

# ==========================================
# Traefik Configuration (early choice)
# ==========================================
ENABLE_TRAEFIK=false

if [ "$UPDATE_MODE" = true ] && is_traefik_configured; then
    # Use existing Traefik config
    ENABLE_TRAEFIK=true
    TRAEFIK_NETWORK=$(get_env "TRAEFIK_NETWORK")
    TRAEFIK_CERTRESOLVER=$(get_env "TRAEFIK_CERTRESOLVER")
    APP_NAME=$(get_env "APP_NAME")
    print_header "🔀 Reverse Proxy Configuration (existing)"
    print_success "Traefik: enabled (network: ${TRAEFIK_NETWORK})"
elif [ "$UPDATE_MODE" = true ]; then
    # Update mode but Traefik not configured - ask if they want to add it
    print_header "🔀 Reverse Proxy Configuration (Traefik)"
    echo ""
    print_info "Traefik is not currently configured."
    if prompt_yes_no "Add Traefik reverse proxy configuration?" "n"; then
        ENABLE_TRAEFIK=true
        echo ""
        TRAEFIK_NETWORK=$(prompt_input "Docker network where Traefik is running" "traefik")
        TRAEFIK_CERTRESOLVER=$(prompt_input "Traefik TLS cert resolver name" "letsencrypt")
        APP_NAME=$(echo "$APP_DNS" | sed 's/\..*//')
        print_success "Traefik configuration added"
    else
        print_info "Keeping standard configuration (port 8080)"
    fi
else
    # Fresh install
    print_header "🔀 Reverse Proxy Configuration (Traefik)"
    echo ""
    print_info "Ackify can be configured to work with Traefik for automatic HTTPS."
    print_info "If you choose Traefik, ports will not be exposed (Traefik will handle routing)."
    echo ""

    if prompt_yes_no "Use Traefik reverse proxy?" "n"; then
        ENABLE_TRAEFIK=true
        echo ""
        TRAEFIK_NETWORK=$(prompt_input "Docker network where Traefik is running" "traefik")
        TRAEFIK_CERTRESOLVER=$(prompt_input "Traefik TLS cert resolver name" "letsencrypt")
        APP_NAME=$(echo "$APP_DNS" | sed 's/\..*//')
        print_success "Traefik configuration completed"
        echo ""
        print_info "Traefik network: ${TRAEFIK_NETWORK}"
        print_info "TLS cert resolver: ${TRAEFIK_CERTRESOLVER}"
        print_info "App name: ${APP_NAME}"
    else
        print_info "Standard configuration selected (port 8080 will be exposed)"
    fi
fi
echo ""

# Download/update configuration files
if [ "$UPDATE_MODE" = true ] && [ -f "compose.yml" ]; then
    print_header "📦 Checking Configuration Files"
    echo ""
    if prompt_yes_no "Update compose.yml to latest version?" "y"; then
        curl -fsSL "https://raw.githubusercontent.com/kolapsis/ackify/main/install/compose.yml.template" -o compose.yml
        print_success "compose.yml template downloaded"

        # Apply region processing based on Traefik choice
        if [ "$ENABLE_TRAEFIK" = true ]; then
            keep_region "compose.yml" "traefik"
            remove_region "compose.yml" "ports"
            print_success "Traefik configuration applied"
        else
            remove_region "compose.yml" "traefik"
            keep_region "compose.yml" "ports"
            print_success "Standard configuration applied (port 8080)"
        fi
        clean_all_markers "compose.yml"
    else
        print_info "Keeping existing compose.yml"
    fi
else
    print_header "📦 Downloading Configuration Files"
    echo ""

    curl -fsSL "https://raw.githubusercontent.com/kolapsis/ackify/main/install/compose.yml.template" -o compose.yml
    print_success "compose.yml template downloaded"

    # Apply region processing based on Traefik choice
    if [ "$ENABLE_TRAEFIK" = true ]; then
        keep_region "compose.yml" "traefik"
        remove_region "compose.yml" "ports"
        print_success "Traefik configuration applied"
    else
        remove_region "compose.yml" "traefik"
        keep_region "compose.yml" "ports"
        print_success "Standard configuration applied (port 8080)"
    fi
    clean_all_markers "compose.yml"

    curl -fsSL https://raw.githubusercontent.com/kolapsis/ackify/main/install/.env.example -o .env.example
    print_success ".env.example downloaded"
fi
echo ""

# ==========================================
# OAuth Configuration
# ==========================================
ENABLE_OAUTH=false

if [ "$UPDATE_MODE" = true ] && is_oauth_configured; then
    # Use existing OAuth config
    ENABLE_OAUTH=true
    OAUTH_PROVIDER=$(get_env "ACKIFY_OAUTH_PROVIDER")
    OAUTH_CLIENT_ID=$(get_env "ACKIFY_OAUTH_CLIENT_ID")
    OAUTH_CLIENT_SECRET=$(get_env "ACKIFY_OAUTH_CLIENT_SECRET")
    OAUTH_ALLOWED_DOMAIN=$(get_env "ACKIFY_OAUTH_ALLOWED_DOMAIN")
    OAUTH_AUTH_URL=$(get_env "ACKIFY_OAUTH_AUTH_URL")
    OAUTH_TOKEN_URL=$(get_env "ACKIFY_OAUTH_TOKEN_URL")
    OAUTH_USERINFO_URL=$(get_env "ACKIFY_OAUTH_USERINFO_URL")
    OAUTH_SCOPES=$(get_env "ACKIFY_OAUTH_SCOPES")
    OAUTH_GITLAB_URL=$(get_env "ACKIFY_OAUTH_GITLAB_URL")
    OAUTH_AUTO_LOGIN=$(get_env "ACKIFY_OAUTH_AUTO_LOGIN")
    print_header "🔑 OAuth2 Configuration (existing)"
    print_success "OAuth2: enabled (${OAUTH_PROVIDER:-custom})"
elif [ "$UPDATE_MODE" = true ]; then
    # Update mode but OAuth not configured - ask if they want to add it
    print_header "🔑 OAuth2 Authentication Configuration"
    echo ""
    print_info "OAuth is not currently configured."
    if prompt_yes_no "Add OAuth2 authentication?" "n"; then
        ENABLE_OAUTH=true
        echo ""
        print_info "Available providers: google, github, gitlab, custom"
        OAUTH_PROVIDER=$(prompt_input "OAuth Provider (google/github/gitlab/custom)" "custom")
        OAUTH_CLIENT_ID=$(prompt_input "OAuth Client ID")
        OAUTH_CLIENT_SECRET=$(prompt_input "OAuth Client Secret")

        echo ""
        if prompt_yes_no "Restrict to specific email domain? (e.g., @company.com)" "n"; then
            OAUTH_ALLOWED_DOMAIN=$(prompt_input "Allowed email domain (e.g., @company.com)" "@example.com")
        else
            OAUTH_ALLOWED_DOMAIN=""
        fi

        if [ "$OAUTH_PROVIDER" = "custom" ]; then
            echo ""
            print_info "Custom OAuth provider configuration"
            OAUTH_AUTH_URL=$(prompt_input "Authorization URL")
            OAUTH_TOKEN_URL=$(prompt_input "Token URL")
            OAUTH_USERINFO_URL=$(prompt_input "User Info URL")
            OAUTH_SCOPES=$(prompt_input "OAuth Scopes" "openid,email,profile")
        fi

        if [ "$OAUTH_PROVIDER" = "gitlab" ]; then
            echo ""
            if prompt_yes_no "Using self-hosted GitLab?" "n"; then
                OAUTH_GITLAB_URL=$(prompt_input "GitLab instance URL" "https://git.company.com")
            fi
        fi

        echo ""
        if prompt_yes_no "Enable auto-login (automatically log in if OAuth session exists)?" "n"; then
            OAUTH_AUTO_LOGIN="true"
        else
            OAUTH_AUTO_LOGIN="false"
        fi
        print_success "OAuth configuration added"
    else
        print_info "OAuth authentication will remain disabled"
    fi
else
    # Fresh install
    print_header "🔑 OAuth2 Authentication Configuration"
    echo ""
    print_info "OAuth allows users to sign in with Google, GitHub, GitLab, or custom provider."
    echo ""

    if prompt_yes_no "Enable OAuth2 authentication?" "y"; then
        ENABLE_OAUTH=true

        echo ""
        print_info "Available providers: google, github, gitlab, custom"
        OAUTH_PROVIDER=$(prompt_input "OAuth Provider (google/github/gitlab/custom)" "custom")

        OAUTH_CLIENT_ID=$(prompt_input "OAuth Client ID")
        OAUTH_CLIENT_SECRET=$(prompt_input "OAuth Client Secret")

        echo ""
        if prompt_yes_no "Restrict to specific email domain? (e.g., @company.com)" "n"; then
            OAUTH_ALLOWED_DOMAIN=$(prompt_input "Allowed email domain (e.g., @company.com)" "@example.com")
        else
            OAUTH_ALLOWED_DOMAIN=""
        fi

        # Custom provider configuration
        if [ "$OAUTH_PROVIDER" = "custom" ]; then
            echo ""
            print_info "Custom OAuth provider configuration"
            OAUTH_AUTH_URL=$(prompt_input "Authorization URL")
            OAUTH_TOKEN_URL=$(prompt_input "Token URL")
            OAUTH_USERINFO_URL=$(prompt_input "User Info URL")
            OAUTH_SCOPES=$(prompt_input "OAuth Scopes" "openid,email,profile")
        fi

        # GitLab self-hosted
        if [ "$OAUTH_PROVIDER" = "gitlab" ]; then
            echo ""
            if prompt_yes_no "Using self-hosted GitLab?" "n"; then
                OAUTH_GITLAB_URL=$(prompt_input "GitLab instance URL" "https://git.company.com")
            fi
        fi

        echo ""
        if prompt_yes_no "Enable auto-login (automatically log in if OAuth session exists)?" "n"; then
            OAUTH_AUTO_LOGIN="true"
        else
            OAUTH_AUTO_LOGIN="false"
        fi

        print_success "OAuth configuration completed"
    else
        print_warning "OAuth authentication disabled"
    fi
fi
echo ""

# ==========================================
# SMTP Configuration
# ==========================================
ENABLE_SMTP=false
ENABLE_MAGICLINK=false

if [ "$UPDATE_MODE" = true ] && is_smtp_configured; then
    # Use existing SMTP config
    ENABLE_SMTP=true
    MAIL_HOST=$(get_env "ACKIFY_MAIL_HOST")
    MAIL_PORT=$(get_env "ACKIFY_MAIL_PORT")
    MAIL_USERNAME=$(get_env "ACKIFY_MAIL_USERNAME")
    MAIL_PASSWORD=$(get_env "ACKIFY_MAIL_PASSWORD")
    MAIL_FROM=$(get_env "ACKIFY_MAIL_FROM")
    MAIL_FROM_NAME=$(get_env "ACKIFY_MAIL_FROM_NAME")
    MAIL_TLS=$(get_env "ACKIFY_MAIL_TLS")
    MAIL_STARTTLS=$(get_env "ACKIFY_MAIL_STARTTLS")

    # Check MagicLink status
    if [ "$(get_env "ACKIFY_AUTH_MAGICLINK_ENABLED")" = "true" ]; then
        ENABLE_MAGICLINK=true
    fi

    print_header "📧 SMTP Configuration (existing)"
    print_success "SMTP: enabled (${MAIL_HOST})"
    if [ "$ENABLE_MAGICLINK" = true ]; then
        print_success "MagicLink: enabled"
    else
        print_info "MagicLink: disabled"
    fi
elif [ "$UPDATE_MODE" = true ]; then
    # Update mode but SMTP not configured - ask if they want to add it
    print_header "📧 SMTP Configuration (Email Service)"
    echo ""
    print_info "SMTP is not currently configured."
    if prompt_yes_no "Add SMTP for email notifications and MagicLink?" "n"; then
        ENABLE_SMTP=true
        echo ""
        MAIL_HOST=$(prompt_input "SMTP Host (e.g., smtp.gmail.com)")
        MAIL_PORT=$(prompt_input "SMTP Port" "587")
        MAIL_USERNAME=$(prompt_input "SMTP Username")
        MAIL_PASSWORD=$(prompt_password "SMTP Password")

        echo ""
        MAIL_FROM=$(prompt_input "From Email Address" "noreply@${APP_DOMAIN}")
        MAIL_FROM_NAME=$(prompt_input "From Name" "$APP_ORGANISATION")

        # Auto-configure TLS based on port
        if [ "$MAIL_PORT" = "465" ]; then
            MAIL_TLS="true"
            MAIL_STARTTLS="false"
        elif [ "$MAIL_PORT" = "587" ]; then
            MAIL_TLS="false"
            MAIL_STARTTLS="true"
        else
            MAIL_TLS="false"
            MAIL_STARTTLS="true"
        fi

        print_success "SMTP configuration added"

        # MagicLink
        echo ""
        ENABLE_MAGICLINK=true
        if prompt_yes_no "Disable MagicLink authentication?" "n"; then
            ENABLE_MAGICLINK=false
        else
            print_success "MagicLink authentication enabled"
        fi
    else
        print_info "SMTP will remain disabled"
    fi
else
    # Fresh install
    print_header "📧 SMTP Configuration (Email Service)"
    echo ""
    print_info "SMTP is used for:"
    print_info "  - Sending signature reminders to expected signers"
    print_info "  - MagicLink authentication (passwordless email login)"
    echo ""

    if prompt_yes_no "Enable SMTP for email notifications and MagicLink?" "y"; then
        ENABLE_SMTP=true

        echo ""
        MAIL_HOST=$(prompt_input "SMTP Host (e.g., smtp.gmail.com)")
        MAIL_PORT=$(prompt_input "SMTP Port" "587")
        MAIL_USERNAME=$(prompt_input "SMTP Username")
        MAIL_PASSWORD=$(prompt_password "SMTP Password")

        echo ""
        MAIL_FROM=$(prompt_input "From Email Address" "noreply@${APP_DOMAIN}")
        MAIL_FROM_NAME=$(prompt_input "From Name" "$APP_ORGANISATION")

        echo ""
        # Auto-configure TLS based on port
        if [ "$MAIL_PORT" = "465" ]; then
            print_info "Port 465 detected - using TLS (implicit SSL)"
            MAIL_TLS="true"
            MAIL_STARTTLS="false"
        elif [ "$MAIL_PORT" = "587" ]; then
            print_info "Port 587 detected - using STARTTLS (explicit TLS)"
            MAIL_TLS="false"
            MAIL_STARTTLS="true"
        else
            print_warning "Non-standard port detected, please configure TLS manually"
            if prompt_yes_no "Use TLS (implicit SSL, typically port 465)?" "n"; then
                MAIL_TLS="true"
                MAIL_STARTTLS="false"
            elif prompt_yes_no "Use STARTTLS (explicit TLS, typically port 587)?" "y"; then
                MAIL_TLS="false"
                MAIL_STARTTLS="true"
            else
                MAIL_TLS="false"
                MAIL_STARTTLS="false"
            fi
        fi

        print_success "SMTP configuration completed"
        echo ""

        # MagicLink configuration
        print_header "🔗 MagicLink Authentication"
        echo ""
        print_info "MagicLink provides passwordless authentication via email."
        print_info "Since SMTP is configured, MagicLink will be enabled by default."
        echo ""

        ENABLE_MAGICLINK=true
        if prompt_yes_no "Disable MagicLink authentication?" "n"; then
            ENABLE_MAGICLINK=false
            print_warning "MagicLink authentication will be disabled"
        else
            print_success "MagicLink authentication enabled"
        fi
    else
        print_warning "SMTP disabled - Email reminders and MagicLink will not be available"
    fi
fi
echo ""

# ==========================================
# Authentication Method Validation
# ==========================================
if [ "$ENABLE_OAUTH" = false ] && [ "$ENABLE_MAGICLINK" = false ]; then
    print_error "At least ONE authentication method must be enabled!"
    print_error "Please enable OAuth or configure SMTP for MagicLink."
    exit 1
fi

# ==========================================
# Telemetry Configuration
# ==========================================
ENABLE_TELEMETRY=false

if [ "$UPDATE_MODE" = true ] && has_env "ACKIFY_TELEMETRY"; then
    # Use existing telemetry setting
    if [ "$(get_env "ACKIFY_TELEMETRY")" = "true" ]; then
        ENABLE_TELEMETRY=true
    fi
    print_header "📊 Telemetry (existing)"
    if [ "$ENABLE_TELEMETRY" = true ]; then
        print_success "Telemetry: enabled"
    else
        print_info "Telemetry: disabled"
    fi
else
    # Fresh install or not configured yet
    print_header "📊 Anonymous Telemetry"
    echo ""
    print_info "Ackify can collect anonymous usage metrics to help improve the project."
    echo ""
    print_info "What is collected (business metrics only):"
    print_info "  - Number of documents created"
    print_info "  - Number of signatures/confirmations"
    print_info "  - Number of webhooks configured"
    print_info "  - Number of email reminders sent"
    echo ""
    print_info "What is NOT collected:"
    print_info "  - No personal data"
    print_info "  - No user information"
    print_info "  - No document content"
    print_info "  - No email addresses"
    print_info "  - No IP addresses"
    echo ""
    print_info "This telemetry is:"
    print_success "  ✓ GDPR compliant"
    print_success "  ✓ Non-intrusive (background only)"
    print_success "  ✓ Helps us improve Ackify for everyone"
    echo ""

    if prompt_yes_no "Enable anonymous telemetry to help improve Ackify?" "y"; then
        ENABLE_TELEMETRY=true
        print_success "Thank you for helping improve Ackify!"
    else
        print_info "Telemetry disabled. You can enable it later in .env (ACKIFY_TELEMETRY=true)"
    fi
fi
echo ""

# ==========================================
# Admin Configuration
# ==========================================
if [ "$UPDATE_MODE" = true ] && has_env "ACKIFY_ADMIN_EMAILS"; then
    # Use existing admin config
    ADMIN_EMAILS=$(get_env "ACKIFY_ADMIN_EMAILS")
    ONLY_ADMIN_CAN_CREATE=$(get_env "ACKIFY_ONLY_ADMIN_CAN_CREATE")
    [ -z "$ONLY_ADMIN_CAN_CREATE" ] && ONLY_ADMIN_CAN_CREATE="false"
    print_header "👤 Admin Configuration (existing)"
    print_success "Admin users: ${ADMIN_EMAILS}"
    if [ "$ONLY_ADMIN_CAN_CREATE" = "true" ]; then
        print_info "Document creation: admins only"
    else
        print_info "Document creation: all users"
    fi
else
    # Fresh install
    print_header "👤 Admin Configuration"
    echo ""
    print_info "Admin users have access to document management and reminder features."
    print_info "At least ONE admin email address is required."
    echo ""

    ADMIN_EMAILS=""
    while [ -z "$ADMIN_EMAILS" ]; do
        ADMIN_EMAILS=$(prompt_input "Admin email addresses (comma-separated)" "admin@${APP_DOMAIN}")

        if [ -z "$ADMIN_EMAILS" ]; then
            print_error "At least one admin email is required!"
            echo ""
        fi
    done

    print_success "Admin users configured: $ADMIN_EMAILS"
    echo ""

    # Document Creation Restriction
    echo ""
    print_info "By default, any authenticated user can create documents."
    print_info "You can restrict document creation to admins only."
    echo ""

    ONLY_ADMIN_CAN_CREATE=false
    if prompt_yes_no "Restrict document creation to admins only?" "n"; then
        ONLY_ADMIN_CAN_CREATE=true
        print_success "Document creation restricted to admins"
    else
        print_success "All authenticated users can create documents"
    fi
fi
echo ""

# ==========================================
# Storage Configuration
# ==========================================
STORAGE_TYPE=""

if [ "$UPDATE_MODE" = true ] && is_storage_configured; then
    # Use existing storage config
    STORAGE_TYPE=$(get_env "ACKIFY_STORAGE_TYPE")
    STORAGE_MAX_SIZE=$(get_env "ACKIFY_STORAGE_MAX_SIZE_MB")
    STORAGE_S3_ENDPOINT=$(get_env "ACKIFY_STORAGE_S3_ENDPOINT")
    STORAGE_S3_BUCKET=$(get_env "ACKIFY_STORAGE_S3_BUCKET")
    STORAGE_S3_ACCESS_KEY=$(get_env "ACKIFY_STORAGE_S3_ACCESS_KEY")
    STORAGE_S3_SECRET_KEY=$(get_env "ACKIFY_STORAGE_S3_SECRET_KEY")
    STORAGE_S3_REGION=$(get_env "ACKIFY_STORAGE_S3_REGION")
    STORAGE_S3_USE_SSL=$(get_env "ACKIFY_STORAGE_S3_USE_SSL")
    print_header "📁 Storage Configuration (existing)"
    if [ "$STORAGE_TYPE" = "local" ]; then
        print_success "Storage: local (max ${STORAGE_MAX_SIZE}MB)"
    elif [ "$STORAGE_TYPE" = "s3" ]; then
        print_success "Storage: S3 (${STORAGE_S3_ENDPOINT})"
    else
        print_info "Storage: disabled"
    fi
elif [ "$UPDATE_MODE" = true ]; then
    # Update mode but storage not configured - ask if they want to add it
    print_header "📁 Document Storage Configuration"
    echo ""
    print_info "Storage is not currently configured."
    if prompt_yes_no "Add document storage configuration?" "n"; then
        echo ""
        print_info "Options: local, s3"
        echo -e "${BLUE}Select storage type (local/s3) [local]: ${NC}"
        read STORAGE_TYPE_INPUT
        STORAGE_TYPE="${STORAGE_TYPE_INPUT:-local}"

        if [ "$STORAGE_TYPE" = "local" ]; then
            STORAGE_MAX_SIZE=$(prompt_input "Maximum file size in MB" "50")
            print_success "Local storage configured"
        elif [ "$STORAGE_TYPE" = "s3" ]; then
            STORAGE_MAX_SIZE=$(prompt_input "Maximum file size in MB" "50")
            echo ""
            STORAGE_S3_ENDPOINT=$(prompt_input "S3 Endpoint")
            STORAGE_S3_BUCKET=$(prompt_input "S3 Bucket name" "ackify-documents")
            STORAGE_S3_ACCESS_KEY=$(prompt_input "S3 Access Key")
            STORAGE_S3_SECRET_KEY=$(prompt_password "S3 Secret Key")
            STORAGE_S3_REGION=$(prompt_input "S3 Region" "us-east-1")
            STORAGE_S3_USE_SSL="true"
            print_success "S3 storage configured"
        fi
    else
        print_info "Storage will remain disabled"
    fi
else
    # Fresh install
    print_header "📁 Document Storage Configuration"
    echo ""
    print_info "Ackify can store uploaded documents locally or in S3-compatible storage."
    print_info "This allows users to upload documents directly instead of providing URLs."
    echo ""
    print_info "Options:"
    print_info "  none  - No document upload (users must provide URLs)"
    print_info "  local - Store documents on the server filesystem"
    print_info "  s3    - Store documents in S3-compatible storage (AWS, MinIO, Wasabi, etc.)"
    echo ""

    echo -e "${BLUE}Select storage type (none/local/s3) [none]: ${NC}"
    read STORAGE_TYPE_INPUT
    STORAGE_TYPE="${STORAGE_TYPE_INPUT:-none}"

    if [ "$STORAGE_TYPE" = "local" ]; then
        STORAGE_MAX_SIZE=$(prompt_input "Maximum file size in MB" "50")
        print_success "Local storage enabled (max ${STORAGE_MAX_SIZE}MB per file)"
    elif [ "$STORAGE_TYPE" = "s3" ]; then
        STORAGE_MAX_SIZE=$(prompt_input "Maximum file size in MB" "50")
        echo ""
        print_info "S3-compatible storage configuration"
        STORAGE_S3_ENDPOINT=$(prompt_input "S3 Endpoint (e.g., https://s3.amazonaws.com or https://minio.example.com)")
        STORAGE_S3_BUCKET=$(prompt_input "S3 Bucket name" "ackify-documents")
        STORAGE_S3_ACCESS_KEY=$(prompt_input "S3 Access Key")
        STORAGE_S3_SECRET_KEY=$(prompt_password "S3 Secret Key")
        STORAGE_S3_REGION=$(prompt_input "S3 Region" "us-east-1")
        if prompt_yes_no "Use SSL for S3 connection?" "y"; then
            STORAGE_S3_USE_SSL="true"
        else
            STORAGE_S3_USE_SSL="false"
        fi
        print_success "S3 storage enabled (${STORAGE_S3_ENDPOINT})"
    else
        STORAGE_TYPE=""
        print_info "Document storage disabled (users must provide URLs)"
    fi
fi
echo ""

# ==========================================
# Generate Secrets
# ==========================================
if [ "$UPDATE_MODE" = true ]; then
    # Preserve existing secrets
    COOKIE_SECRET=$(get_env "ACKIFY_OAUTH_COOKIE_SECRET")
    ED25519_KEY=$(get_env "ACKIFY_ED25519_PRIVATE_KEY")
    DB_PASSWORD=$(get_env "POSTGRES_PASSWORD")
    DB_APP_PASSWORD=$(get_env "ACKIFY_APP_PASSWORD")
    print_header "🔑 Security Secrets (preserved)"
    print_success "Existing secrets preserved"
else
    # Generate new secrets
    print_header "🔑 Generating Secure Secrets"
    echo ""

    COOKIE_SECRET=$(openssl rand -base64 32)
    print_success "Cookie secret generated"

    ED25519_KEY=$(openssl rand 64 | base64 -w 0)
    print_success "Ed25519 private key generated"

    DB_PASSWORD=$(openssl rand -hex 24)
    print_success "Database password generated"

    DB_APP_PASSWORD=$(openssl rand -hex 24)
    print_success "App database password generated"
fi
echo ""

# ==========================================
# Create .env file
# ==========================================
if [ "$UPDATE_MODE" = true ]; then
    print_header "📝 Updating Configuration File"
    # Backup existing .env
    cp .env ".env.backup.$(date +%Y%m%d_%H%M%S)"
    print_success "Existing .env backed up"
else
    print_header "📝 Creating Configuration File"
fi
echo ""

cat > .env <<EOF
# ==========================================
# Ackify Community Edition Configuration
# Generated on $(date)
# ==========================================

# ==========================================
# Application Configuration
# ==========================================
ACKIFY_BASE_URL=${APP_BASE_URL}
ACKIFY_ORGANISATION=${APP_ORGANISATION}
ACKIFY_ORGANISATION_DOMAIN=${APP_ORGANISATION_DOMAIN}

# ==========================================
# Database Configuration
# ==========================================
POSTGRES_PASSWORD=${DB_PASSWORD}
ACKIFY_APP_PASSWORD=${DB_APP_PASSWORD}

# ==========================================
# Security Configuration (Auto-generated)
# ==========================================
ACKIFY_OAUTH_COOKIE_SECRET=${COOKIE_SECRET}
ACKIFY_ED25519_PRIVATE_KEY=${ED25519_KEY}

# ==========================================
# Server Configuration
# ==========================================
ACKIFY_LISTEN_ADDR=:8080
ACKIFY_LOG_LEVEL=info

EOF

# OAuth configuration
if [ "$ENABLE_OAUTH" = true ]; then
    cat >> .env <<EOF
# ==========================================
# OAuth2 Configuration
# ==========================================
ACKIFY_OAUTH_PROVIDER=${OAUTH_PROVIDER}
ACKIFY_OAUTH_CLIENT_ID=${OAUTH_CLIENT_ID}
ACKIFY_OAUTH_CLIENT_SECRET=${OAUTH_CLIENT_SECRET}
EOF

    if [ -n "$OAUTH_ALLOWED_DOMAIN" ]; then
        echo "ACKIFY_OAUTH_ALLOWED_DOMAIN=${OAUTH_ALLOWED_DOMAIN}" >> .env
    fi

    if [ "$OAUTH_PROVIDER" = "custom" ]; then
        cat >> .env <<EOF
ACKIFY_OAUTH_AUTH_URL=${OAUTH_AUTH_URL}
ACKIFY_OAUTH_TOKEN_URL=${OAUTH_TOKEN_URL}
ACKIFY_OAUTH_USERINFO_URL=${OAUTH_USERINFO_URL}
ACKIFY_OAUTH_SCOPES=${OAUTH_SCOPES}
EOF
    fi

    if [ -n "$OAUTH_GITLAB_URL" ]; then
        echo "ACKIFY_OAUTH_GITLAB_URL=${OAUTH_GITLAB_URL}" >> .env
    fi

    echo "ACKIFY_OAUTH_AUTO_LOGIN=${OAUTH_AUTO_LOGIN}" >> .env
    echo "ACKIFY_AUTH_OAUTH_ENABLED=true" >> .env
    echo "" >> .env
else
    # OAuth not configured - explicitly disable
    echo "# OAuth2 not configured" >> .env
    echo "ACKIFY_AUTH_OAUTH_ENABLED=false" >> .env
    echo "" >> .env
fi

# SMTP configuration
if [ "$ENABLE_SMTP" = true ]; then
    cat >> .env <<EOF
# ==========================================
# SMTP Configuration (Email Service)
# ==========================================
ACKIFY_MAIL_HOST=${MAIL_HOST}
ACKIFY_MAIL_PORT=${MAIL_PORT}
ACKIFY_MAIL_USERNAME=${MAIL_USERNAME}
ACKIFY_MAIL_PASSWORD=${MAIL_PASSWORD}
ACKIFY_MAIL_FROM=${MAIL_FROM}
ACKIFY_MAIL_FROM_NAME=${MAIL_FROM_NAME}
ACKIFY_MAIL_TLS=${MAIL_TLS}
ACKIFY_MAIL_STARTTLS=${MAIL_STARTTLS}
ACKIFY_MAIL_TIMEOUT=10s
ACKIFY_MAIL_TEMPLATE_DIR=templates
ACKIFY_MAIL_DEFAULT_LOCALE=en

EOF
fi

# MagicLink configuration
if [ "$ENABLE_MAGICLINK" = true ]; then
    echo "# MagicLink authentication enabled" >> .env
    echo "ACKIFY_AUTH_MAGICLINK_ENABLED=true" >> .env
    echo "" >> .env
else
    echo "# MagicLink authentication disabled" >> .env
    echo "ACKIFY_AUTH_MAGICLINK_ENABLED=false" >> .env
    echo "" >> .env
fi

# Admin configuration
if [ -n "$ADMIN_EMAILS" ]; then
    cat >> .env <<EOF
# ==========================================
# Admin Configuration
# ==========================================
ACKIFY_ADMIN_EMAILS=${ADMIN_EMAILS}

# Restrict document creation to admins only (default: false)
ACKIFY_ONLY_ADMIN_CAN_CREATE=${ONLY_ADMIN_CAN_CREATE}

EOF
fi

# Traefik configuration
if [ "$ENABLE_TRAEFIK" = true ]; then
    cat >> .env <<EOF
# ==========================================
# Traefik Configuration
# ==========================================
TRAEFIK_NETWORK=${TRAEFIK_NETWORK}
TRAEFIK_CERTRESOLVER=${TRAEFIK_CERTRESOLVER}
APP_NAME=${APP_NAME}
APP_DNS=${APP_DNS}

EOF
fi

# Storage configuration
if [ -n "$STORAGE_TYPE" ]; then
    cat >> .env <<EOF
# ==========================================
# Document Storage Configuration
# ==========================================
ACKIFY_STORAGE_TYPE=${STORAGE_TYPE}
ACKIFY_STORAGE_MAX_SIZE_MB=${STORAGE_MAX_SIZE}
EOF

    if [ "$STORAGE_TYPE" = "local" ]; then
        echo "ACKIFY_STORAGE_LOCAL_PATH=/data/documents" >> .env
    elif [ "$STORAGE_TYPE" = "s3" ]; then
        cat >> .env <<EOF
ACKIFY_STORAGE_S3_ENDPOINT=${STORAGE_S3_ENDPOINT}
ACKIFY_STORAGE_S3_BUCKET=${STORAGE_S3_BUCKET}
ACKIFY_STORAGE_S3_ACCESS_KEY=${STORAGE_S3_ACCESS_KEY}
ACKIFY_STORAGE_S3_SECRET_KEY=${STORAGE_S3_SECRET_KEY}
ACKIFY_STORAGE_S3_REGION=${STORAGE_S3_REGION}
ACKIFY_STORAGE_S3_USE_SSL=${STORAGE_S3_USE_SSL}
EOF
    fi
    echo "" >> .env
else
    echo "# Document storage disabled (users must provide URLs)" >> .env
    echo "" >> .env
fi

# Telemetry configuration
cat >> .env <<EOF
# ==========================================
# Telemetry Configuration
# ==========================================
# Anonymous usage metrics (GDPR compliant, no personal data)
ACKIFY_TELEMETRY=${ENABLE_TELEMETRY}
EOF

# Add data dir and create host directory if telemetry is enabled
if [ "$ENABLE_TELEMETRY" = true ]; then
    echo "# Data directory for identity file (bind mount from host to persist across container recreations)" >> .env
    echo "ACKIFY_TELEMETRY_DATA_DIR=/data/telemetry" >> .env
    # Create the telemetry directory on host
    mkdir -p telemetry
    print_success "Created telemetry directory for identity persistence"
fi
echo "" >> .env

print_success ".env file created successfully"
echo ""

# ==========================================
# Installation Summary
# ==========================================
if [ "$UPDATE_MODE" = true ]; then
    print_header "📊 Update Summary"
else
    print_header "📊 Installation Summary"
fi
echo ""

print_info "Base URL: ${APP_BASE_URL}"
print_info "Organization: ${APP_ORGANISATION}"
echo ""

print_info "Authentication Methods:"
if [ "$ENABLE_OAUTH" = true ]; then
    print_success "  ✓ OAuth2 ($OAUTH_PROVIDER)"
else
    print_warning "  ✗ OAuth2 (disabled)"
fi

if [ "$ENABLE_MAGICLINK" = "true" ]; then
    print_success "  ✓ MagicLink (passwordless email)"
else
    print_warning "  ✗ MagicLink (disabled)"
fi
echo ""

if [ "$ENABLE_SMTP" = true ]; then
    print_success "Email Service: Enabled (${MAIL_HOST})"
else
    print_warning "Email Service: Disabled"
fi
echo ""

print_success "Admin Users: ${ADMIN_EMAILS}"
if [ "$ONLY_ADMIN_CAN_CREATE" = true ]; then
    print_info "Document Creation: Restricted to admins only"
else
    print_info "Document Creation: All authenticated users"
fi
echo ""

if [ "$ENABLE_TRAEFIK" = true ]; then
    print_success "Reverse Proxy: Traefik (network: ${TRAEFIK_NETWORK})"
    print_info "TLS Certificate: ${TRAEFIK_CERTRESOLVER}"
else
    print_info "Reverse Proxy: None (direct port 8080 exposure)"
fi
echo ""

if [ "$STORAGE_TYPE" = "local" ]; then
    print_success "Document Storage: Local filesystem (max ${STORAGE_MAX_SIZE}MB)"
elif [ "$STORAGE_TYPE" = "s3" ]; then
    print_success "Document Storage: S3 (${STORAGE_S3_ENDPOINT}, max ${STORAGE_MAX_SIZE}MB)"
else
    print_info "Document Storage: Disabled (URL-only mode)"
fi
echo ""

if [ "$ENABLE_TELEMETRY" = true ]; then
    print_success "Telemetry: Enabled (thank you!)"
    print_info "  Identity persisted in ./telemetry/ (host bind mount)"
else
    print_info "Telemetry: Disabled"
fi
echo ""

# ==========================================
# Next Steps
# ==========================================
print_header "🚀 Next Steps"
echo ""

if [ "$UPDATE_MODE" = true ]; then
    print_info "1. Review configuration changes:"
    echo "   diff .env.backup.* .env"
    echo ""

    print_info "2. Restart Ackify to apply changes:"
    echo "   docker compose down && docker compose up -d"
    echo ""

    print_info "3. Check logs:"
    echo "   docker compose logs -f ackify-ce"
    echo ""

    print_header "✅ Update Complete!"
    echo ""
    print_success "Configuration file: $(pwd)/.env"
    print_success "Backup file: $(pwd)/.env.backup.*"
else
    print_info "1. Review configuration:"
    echo "   cat .env"
    echo ""

    print_info "2. Start Ackify:"
    echo "   docker compose up -d"
    echo ""

    print_info "3. Check logs:"
    echo "   docker compose logs -f ackify-ce"
    echo ""

    print_info "4. Access application:"
    echo "   ${APP_BASE_URL}"
    echo ""

    print_info "5. Check health:"
    echo "   curl ${APP_BASE_URL}/health"
    echo ""

    print_header "✅ Installation Complete!"
    echo ""
    print_success "Installation directory: $(pwd)"
    print_success "Configuration file: $(pwd)/.env"
fi
echo ""

if [ "$ENABLE_OAUTH" = false ] && [ "$ENABLE_MAGICLINK" = true ]; then
    print_warning "Note: Only MagicLink is enabled. Users will need to receive an email to sign in."
    echo ""
fi

if [ "$UPDATE_MODE" = true ]; then
    print_info "Ready to apply changes? Run: docker compose down && docker compose up -d"
else
    print_info "Ready to start Ackify? Run: docker compose up -d"
fi
echo ""
