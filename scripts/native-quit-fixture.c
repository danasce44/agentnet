// Activate only the disposable fixture's real Quit menu after its report.
// Never count a forced signal or the parent's status alone as renderer evidence.
#define _GNU_SOURCE
#include <gtk/gtk.h>
#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
static gboolean quit_ready(gpointer item) {
 const char *marker=getenv("AGENTNET_FIXTURE_QUIT_MARKER");
 if(!marker || access(marker,F_OK))return G_SOURCE_CONTINUE;
 fprintf(stderr,"FIXTURE real Quit AgentNet menu activated\n");
 gtk_menu_item_activate(GTK_MENU_ITEM(item));g_object_unref(item);return G_SOURCE_REMOVE;
}
void gtk_menu_shell_append(GtkMenuShell *menu,GtkWidget *item) {
 static void (*real)(GtkMenuShell*,GtkWidget*);
 if(!real)real=dlsym(RTLD_NEXT,"gtk_menu_shell_append");
 real(menu,item);
 if(getenv("AGENTNET_FIXTURE_QUIT_MARKER") && GTK_IS_MENU_ITEM(item)) {
  const char *label=gtk_menu_item_get_label(GTK_MENU_ITEM(item));
  if(label && !strcmp(label,"Quit AgentNet")) { g_object_ref(item);g_timeout_add(20,quit_ready,item); }
 }
}
